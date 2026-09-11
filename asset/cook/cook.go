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

	"github.com/riftbane/veduta/asset"
	"github.com/riftbane/veduta/asset/model"
	"github.com/riftbane/veduta/asset/texture"
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
	c, err := newCooker(root, p)
	if err != nil {
		return nil, err
	}
	if err := c.run(); err != nil {
		return nil, err
	}
	if errs := c.report.Errors(); len(errs) > 0 {
		return nil, errs
	}
	return c.lib, nil
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
	for _, k := range asset.CookedKinds {
		names, err := c.sources(k)
		if err != nil {
			return err
		}
		for _, file := range names {
			c.cookOne(k, file)
		}
		if err := c.prune(k, names); err != nil {
			return err
		}
	}
	c.report.Warnings = c.lib.References()
	return nil
}

// sources lists the source files of kind k (base names, sorted).
func (c *cooker) sources(k asset.Kind) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(c.assets, filepath.FromSlash(k.Dir())))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cook: %w", err)
	}
	var out []string
	for _, e := range entries {
		if _, ok := k.NameFromFile(e.Name()); ok && !e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

// rel returns p relative to the project root with forward slashes.
func (c *cooker) rel(p string) string {
	r, err := filepath.Rel(c.root, p)
	if err != nil {
		return filepath.ToSlash(p)
	}
	return filepath.ToSlash(r)
}

func (c *cooker) cookOne(k asset.Kind, base string) {
	name, _ := k.NameFromFile(base)
	srcAbs := filepath.Join(c.assets, filepath.FromSlash(k.Dir()), base)
	srcAssetRel := path.Join(k.Dir(), base) // relative to the assets directory
	outAbs := filepath.Join(c.cooked, filepath.FromSlash(k.Dir()), name+".vda")
	it := Item{Kind: k, Name: name, Source: c.rel(srcAbs), Output: c.rel(outAbs)}
	fail := func(err error) {
		it.Status = StatusError
		it.Errors = toSourceErrors(it.Source, err)
		c.report.Failed++
		c.report.Items = append(c.report.Items, it)
	}
	data, err := os.ReadFile(srcAbs)
	if err != nil {
		fail(err)
		return
	}
	var deps []string
	var texSrc asset.TextureSource
	var texLoc *asset.Locator
	if k == asset.KindTexture {
		texLoc, err = asset.Decode(it.Source, data, asset.TypeTexture, &texSrc)
		if err != nil {
			fail(err)
			return
		}
		deps = texture.Deps(&texSrc)
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
		n, _ := k.NameFromFile(s)
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
