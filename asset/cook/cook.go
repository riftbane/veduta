// Package cook compiles a project's asset sources into .vda files incrementally and
// loads a project's assets for the game at startup.
//
// Cooked files live under the project's cooked directory (default assets/.cooked) as
// <kind dir>/<name>.vda, for example assets/.cooked/models/crate.vda. A file is fresh when
// its META chunk carries the current compiler version and the hash of the current
// inputs (source file plus dependencies such as texture image layers); Run recompiles
// only stale files and removes cooked files whose source is gone. Load never writes: it
// uses fresh cooked files and compiles stale sources in memory.
package cook

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/riftbane/veduta/v2/asset"
	"github.com/riftbane/veduta/v2/asset/model"
	"github.com/riftbane/veduta/v2/asset/texture"
)

// Status of one asset after Run.
type Status string

// Statuses.
const (
	StatusCompiled Status = "compiled" // (re)compiled and written
	StatusFresh    Status = "fresh"    // cooked file already up to date
	StatusStale    Status = "stale"    // needs compiling (dry run)
	StatusError    Status = "error"    // the source does not compile
	StatusRemoved  Status = "removed"  // cooked file without a source, deleted
)

// Item is the outcome for one asset.
type Item struct {
	Kind   asset.Kind           `json:"kind"`
	Name   string               `json:"name"`
	Source string               `json:"source"`           // path relative to the project root
	Output string               `json:"output,omitempty"` // path relative to the project root
	Hash   string               `json:"hash,omitempty"`
	Status Status               `json:"status"`
	Errors []*asset.SourceError `json:"errors,omitempty"`
}

// Report summarizes a Run.
type Report struct {
	Items    []Item   `json:"items"`
	Compiled int      `json:"compiled"`
	Fresh    int      `json:"fresh"`
	Stale    int      `json:"stale"`
	Failed   int      `json:"failed"`
	Removed  int      `json:"removed"`
	Warnings []string `json:"warnings"`
}

// Errors returns every source error of the report.
func (r *Report) Errors() asset.Errors {
	var out asset.Errors
	for _, it := range r.Items {
		out = append(out, it.Errors...)
	}
	return out
}

// Options configures Run.
type Options struct {
	Root    string         // project root (the directory holding veduta.json)
	Project *asset.Project // parsed manifest; nil reads Root/veduta.json
	Force   bool           // recompile everything
	DryRun  bool           // only report fresh/stale, write nothing
}

// ReadProject parses root/veduta.json.
func ReadProject(root string) (*asset.Project, error) {
	file := filepath.Join(root, asset.ProjectFile)
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("read project: %w", err)
	}
	return asset.ParseProject(asset.ProjectFile, data)
}

// Run cooks the project at opt.Root.
func Run(opt Options) (*Report, error) {
	c, err := newCooker(opt.Root, opt.Project)
	if err != nil {
		return nil, err
	}
	c.write, c.force, c.dry = !opt.DryRun, opt.Force, opt.DryRun
	if err := c.run(); err != nil {
		return nil, err
	}
	return c.report, nil
}

// Load returns every asset of the project at root: fresh cooked files are decoded,
// stale or missing ones are compiled from source in memory. Compile errors of any asset
// are returned together as asset.Errors.
func Load(root string) (*asset.Library, error) {
	return LoadProject(root, nil)
}

// LoadProject is Load with an already parsed manifest (nil reads it).
func LoadProject(root string, p *asset.Project) (*asset.Library, error) {
	lib, errs, err := LoadPartial(root, p)
	if err != nil {
		return nil, err
	}
	if len(errs) > 0 {
		return nil, errs
	}
	return lib, nil
}

// LoadPartial is LoadProject that keeps going: it returns every asset that compiles and
// the located errors of those that do not (inspection of one asset should not be blocked
// by another broken one). The error is only for I/O problems.
func LoadPartial(root string, p *asset.Project) (*asset.Library, asset.Errors, error) {
	c, err := newCooker(root, p)
	if err != nil {
		return nil, nil, err
	}
	if err := c.run(); err != nil {
		return nil, nil, err
	}
	return c.lib, c.report.Errors(), nil
}

type cooker struct {
	root   string
	assets string // absolute assets dir
	cooked string // absolute cooked dir
	proj   *asset.Project
	write  bool
	force  bool
	dry    bool
	report *Report
	lib    *asset.Library
	// files are the sources of each kind by asset name, as slash paths under the kind's
	// directory ("enemies/bat.mat.json"); dups are the later sources of a name already
	// taken, each with the path that took it.
	files map[asset.Kind]map[string]string
	dups  map[asset.Kind]map[string]string
}

func newCooker(root string, p *asset.Project) (*cooker, error) {
	if p == nil {
		var err error
		if p, err = ReadProject(root); err != nil {
			return nil, err
		}
	}
	c := &cooker{
		root:   root,
		assets: filepath.Join(root, filepath.FromSlash(p.Assets)),
		cooked: filepath.Join(root, filepath.FromSlash(p.Cooked)),
		proj:   p,
		report: &Report{Items: []Item{}, Warnings: []string{}},
		lib:    asset.NewLibrary(p),
	}
	return c, nil
}

func (c *cooker) run() error {
	sources := map[asset.Kind][]string{}
	for _, k := range asset.CookedKinds {
		files, err := c.sources(k)
		if err != nil {
			return err
		}
		sources[k] = files
	}
	for _, k := range asset.CookedKinds {
		for _, file := range sources[k] {
			c.cookOne(k, file)
		}
		if err := c.prune(k, sources[k]); err != nil {
			return err
		}
	}
	c.report.Warnings = c.lib.References()
	return nil
}

// sources lists the source files of kind k as slash paths under the kind's directory,
// sorted: sources may sit in folders of their own (materials/enemies/bat.mat.json), and a
// source's asset name is still its file name. It also indexes them by name, and notes a
// name used twice.
func (c *cooker) sources(k asset.Kind) ([]string, error) {
	files, err := SourceFiles(c.assets, k)
	if err != nil {
		return nil, err
	}
	if c.files == nil {
		c.files, c.dups = map[asset.Kind]map[string]string{}, map[asset.Kind]map[string]string{}
	}
	c.files[k], c.dups[k] = map[string]string{}, map[string]string{}
	for _, f := range files {
		name, _ := k.NameFromFile(path.Base(f))
		if first, ok := c.files[k][name]; ok {
			c.dups[k][f] = first
			continue
		}
		c.files[k][name] = f
	}
	return files, nil
}

// SourceFiles lists the source files of kind k under the assets directory, as slash paths
// under the kind's directory, sorted, searching its folders too (hidden ones excepted).
func SourceFiles(assets string, k asset.Kind) ([]string, error) {
	dir := filepath.Join(assets, filepath.FromSlash(k.Dir()))
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && p == dir {
				return filepath.SkipAll
			}
			return err
		}
		if d.IsDir() {
			if p != dir && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if _, ok := k.NameFromFile(d.Name()); !ok {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("cook: %w", err)
	}
	sort.Strings(out)
	return out, nil
}

// SourcePath returns the source file of asset name of kind k, relative to the project root
// with forward slashes: where it is, in a folder or not, or where a new one would go.
func SourcePath(root string, p *asset.Project, k asset.Kind, name string) string {
	base := name + k.Ext()
	if files, err := SourceFiles(filepath.Join(root, filepath.FromSlash(p.Assets)), k); err == nil {
		for _, f := range files {
			if path.Base(f) == base {
				return path.Join(p.Assets, k.Dir(), f)
			}
		}
	}
	return path.Join(p.Assets, k.Dir(), base)
}

// rel returns p relative to the project root with forward slashes.
func (c *cooker) rel(p string) string {
	r, err := filepath.Rel(c.root, p)
	if err != nil {
		return filepath.ToSlash(p)
	}
	return filepath.ToSlash(r)
}

func (c *cooker) cookOne(k asset.Kind, file string) {
	name, _ := k.NameFromFile(path.Base(file))
	srcAbs := filepath.Join(c.assets, filepath.FromSlash(k.Dir()), filepath.FromSlash(file))
	srcAssetRel := path.Join(k.Dir(), file) // relative to the assets directory
	outAbs := filepath.Join(c.cooked, filepath.FromSlash(k.Dir()), name+".vda")
	it := Item{Kind: k, Name: name, Source: c.rel(srcAbs), Output: c.rel(outAbs)}
	fail := func(err error) {
		it.Status = StatusError
		it.Errors = toSourceErrors(it.Source, err)
		c.report.Failed++
		c.report.Items = append(c.report.Items, it)
	}
	if first, dup := c.dups[k][file]; dup {
		fail(fmt.Errorf("the %s name %q is taken by %s: an asset's name is its file name, whatever its folder, so it must be unique",
			k, name, path.Join(k.Dir(), first)))
		return
	}
	data, err := os.ReadFile(srcAbs)
	if err != nil {
		fail(err)
		return
	}
	var deps []string
	var texSrc asset.TextureSource
	var texLoc *asset.Locator
	var worldSrc asset.WorldSource
	var worldLoc *asset.Locator
	switch k {
	case asset.KindTexture:
		texLoc, err = asset.Decode(it.Source, data, asset.TypeTexture, &texSrc)
		if err != nil {
			fail(err)
			return
		}
		deps = texture.Deps(&texSrc)
	case asset.KindWorld:
		// A world is compiled against its prefabs' footprints, so it depends on them.
		worldLoc, err = asset.Decode(it.Source, data, asset.TypeWorld, &worldSrc)
		if err != nil {
			fail(err)
			return
		}
		deps = asset.WorldDeps(&worldSrc)
		for i, d := range deps { // a prefab in a folder of its own
			if rest, ok := strings.CutPrefix(d, asset.KindPrefab.Dir()+"/"); ok {
				if n, ok := asset.KindPrefab.NameFromFile(rest); ok && c.files[asset.KindPrefab][n] != "" {
					deps[i] = path.Join(asset.KindPrefab.Dir(), c.files[asset.KindPrefab][n])
				}
			}
		}
	}
	hash, err := c.hash(srcAssetRel, data, deps)
	if err != nil {
		fail(err)
		return
	}
	it.Hash = hash

	if !c.force {
		if body, ok := c.fresh(outAbs, hash); ok {
			if err := c.decodeInto(k, name, body); err == nil {
				it.Status = StatusFresh
				c.report.Fresh++
				c.report.Items = append(c.report.Items, it)
				return
			}
		}
	}
	if c.dry {
		it.Status = StatusStale
		c.report.Stale++
		c.report.Items = append(c.report.Items, it)
		return
	}
	var body asset.Chunk
	switch k {
	case asset.KindTexture:
		// os.Root confines image layers to the assets directory, symlinks included.
		root, err := os.OpenRoot(c.assets)
		if err != nil {
			fail(err)
			return
		}
		t, err := texture.Compile(name, &texSrc, texLoc, texture.Options{FS: root.FS()})
		root.Close()
		if err != nil {
			fail(err)
			return
		}
		c.lib.Textures[name] = t
		body = asset.EncodeTexture(t)
	case asset.KindMaterial:
		m, err := asset.ParseMaterial(it.Source, data)
		if err != nil {
			fail(err)
			return
		}
		c.lib.Materials[name] = m
		body = asset.EncodeMaterial(m)
	case asset.KindModel:
		m, err := model.Parse(it.Source, data)
		if err != nil {
			fail(err)
			return
		}
		c.lib.Models[name] = m
		body = asset.EncodeModel(m)
	case asset.KindScene:
		s, err := asset.ParseScene(it.Source, data)
		if err != nil {
			fail(err)
			return
		}
		c.lib.Scenes[name] = s
		body = asset.EncodeScene(s)
	case asset.KindPrefab:
		p, err := asset.ParsePrefab(it.Source, data)
		if err != nil {
			fail(err)
			return
		}
		c.lib.Prefabs[name] = p
		body = asset.EncodePrefab(p)
	case asset.KindWorld:
		w, err := asset.CompileWorld(name, &worldSrc, worldLoc, c.lib.Prefab)
		if err != nil {
			fail(err)
			return
		}
		c.lib.Worlds[name] = w
		body = asset.EncodeWorld(w)
	}
	if c.write {
		meta := asset.Meta{Kind: k, Name: name, Source: srcAssetRel, SourceHash: hash, Compiler: asset.CompilerVersion, Deps: deps}
		vda, err := asset.PackVDA(meta, body)
		if err == nil {
			err = writeAtomic(outAbs, vda)
		}
		if err != nil {
			fail(err)
			return
		}
	}
	it.Status = StatusCompiled
	c.report.Compiled++
	c.report.Items = append(c.report.Items, it)
}

// fresh returns the body chunk of the cooked file at p when it matches hash and the
// current compiler.
func (c *cooker) fresh(p, hash string) (asset.Chunk, bool) {
	data, err := os.ReadFile(p)
	if err != nil {
		return asset.Chunk{}, false
	}
	meta, body, err := asset.UnpackVDA(data)
	if err != nil || meta.SourceHash != hash || meta.Compiler != asset.CompilerVersion {
		return asset.Chunk{}, false
	}
	return body, true
}

func (c *cooker) decodeInto(k asset.Kind, name string, body asset.Chunk) error {
	switch k {
	case asset.KindTexture:
		t, err := asset.DecodeTexture(body)
		if err != nil {
			return err
		}
		t.Name = name
		c.lib.Textures[name] = t
	case asset.KindMaterial:
		m, err := asset.DecodeMaterial(body)
		if err != nil {
			return err
		}
		m.Name = name
		c.lib.Materials[name] = m
	case asset.KindModel:
		m, err := asset.DecodeModel(body)
		if err != nil {
			return err
		}
		m.Name = name
		c.lib.Models[name] = m
	case asset.KindScene:
		s, err := asset.DecodeScene(body)
		if err != nil {
			return err
		}
		s.Name = name
		c.lib.Scenes[name] = s
	case asset.KindPrefab:
		p, err := asset.DecodePrefab(body)
		if err != nil {
			return err
		}
		p.Name = name
		c.lib.Prefabs[name] = p
	case asset.KindWorld:
		w, err := asset.DecodeWorld(body)
		if err != nil {
			return err
		}
		w.Name = name
		c.lib.Worlds[name] = w
	}
	return nil
}

// hash returns the input hash of an asset: SHA-256 over a version line, the compiler
// version, then the source and each dependency as "<tag> <path> <length>\n<bytes>".
// Missing dependencies hash as "missing <path>\n" (compiling then reports the error).
func (c *cooker) hash(source string, data []byte, deps []string) (string, error) {
	h := sha256.New()
	fmt.Fprintf(h, "veduta-cook/1\n%s\n", asset.CompilerVersion)
	fmt.Fprintf(h, "source %s %d\n", source, len(data))
	h.Write(data)
	for _, d := range deps {
		b, err := os.ReadFile(filepath.Join(c.assets, filepath.FromSlash(d)))
		if err != nil {
			fmt.Fprintf(h, "missing %s\n", d)
			continue
		}
		fmt.Fprintf(h, "dep %s %d\n", d, len(b))
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// prune deletes cooked files of kind k whose source no longer exists.
func (c *cooker) prune(k asset.Kind, sources []string) error {
	dir := filepath.Join(c.cooked, filepath.FromSlash(k.Dir()))
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cook: %w", err)
	}
	have := map[string]bool{}
	for _, s := range sources {
		n, _ := k.NameFromFile(path.Base(s))
		have[n] = true
	}
	for _, e := range entries {
		n, ok := strings.CutSuffix(e.Name(), ".vda")
		if !ok || e.IsDir() || have[n] {
			continue
		}
		p := filepath.Join(dir, e.Name())
		it := Item{Kind: k, Name: n, Output: c.rel(p), Status: StatusRemoved}
		if c.write {
			if err := os.Remove(p); err != nil {
				return fmt.Errorf("cook: %w", err)
			}
			c.report.Removed++
			c.report.Items = append(c.report.Items, it)
		}
	}
	return nil
}

func writeAtomic(p string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".tmp-*.vda")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), p); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

// toSourceErrors converts any error into located source errors.
func toSourceErrors(file string, err error) []*asset.SourceError {
	var list asset.Errors
	var one *asset.SourceError
	switch {
	case errors.As(err, &list):
		return list
	case errors.As(err, &one):
		return []*asset.SourceError{one}
	}
	return []*asset.SourceError{{File: file, Msg: err.Error()}}
}
