package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/riftbane/veduta/v2/asset"
	"github.com/riftbane/veduta/v2/asset/cook"
	"github.com/riftbane/veduta/v2/inspect"
	"github.com/riftbane/veduta/v2/mcp"
)

// InspectResult wraps an inspection report for printing.
type InspectResult struct{ *inspect.Report }

// ExitCode is 1 when the report has errors.
func (r InspectResult) ExitCode() int {
	if r.Summary.Errors > 0 {
		return 1
	}
	return 0
}

// Human prints the summary, the issues and the sheets.
func (r InspectResult) Human() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %d errors, %d warnings, %d info\n", r.Subject, r.Summary.Errors, r.Summary.Warnings, r.Summary.Info)
	for _, is := range r.Issues {
		fmt.Fprintf(&b, "  %-7s %s ×%d", is.Severity, is.Code, is.Count)
		if len(is.Where) > 0 {
			w, _ := json.Marshal(is.Where)
			fmt.Fprintf(&b, " %s", w)
		}
		fmt.Fprintf(&b, "\n          %s\n", is.Hint)
	}
	for _, s := range r.Sheets {
		fmt.Fprintf(&b, "sheet: %s\n", s)
	}
	return b.String()
}

// InspectKinds are the inspectable asset kinds.
var InspectKinds = []string{"model", "texture", "scene", "prefab", "world", "map"}

// Inspect produces the report and sheets of one model, texture, scene, prefab, world or map.
func (s *Session) Inspect(kind, name, focus string, sheets []string) (*inspect.Report, error) {
	lib, errs, err := s.PartialLibrary()
	if err != nil {
		return nil, err
	}
	var k asset.Kind
	switch kind {
	case "model":
		k = asset.KindModel
	case "texture":
		k = asset.KindTexture
	case "scene":
		k = asset.KindScene
	case "prefab":
		k = asset.KindPrefab
	case "world":
		k = asset.KindWorld
	case "map":
		k = asset.KindMap
	default:
		return nil, usagef("inspect: kind %q (want one of %s)", kind, strings.Join(InspectKinds, ", "))
	}
	if err := asset.ValidName(name); err != nil {
		return nil, usagef("inspect: %v", err)
	}
	// When the subject itself does not compile, the located errors are the report.
	src := cook.SourcePath(s.Root, s.Project, k, name)
	var own asset.Errors
	for _, e := range errs {
		if strings.HasSuffix(e.File, src) {
			own = append(own, e)
		}
	}
	if len(own) > 0 {
		return nil, fmt.Errorf("%s %s does not compile: %w", kind, name, own)
	}
	ir, err := inspect.NewRenderer(lib)
	if err != nil {
		return nil, err
	}
	defer ir.Close()
	opt := inspect.Options{OutDir: s.Out(), Focus: focus, Sheets: sheets}
	var rep *inspect.Report
	switch kind {
	case "model":
		rep, err = inspect.Model(ir, name, opt)
	case "texture":
		assets := filepath.Join(s.Root, filepath.FromSlash(s.Project.Assets))
		file := filepath.Join(assets, "textures", name+asset.KindTexture.Ext())
		var ts *inspect.TexSource
		if data, rerr := os.ReadFile(file); rerr == nil {
			if root, oerr := os.OpenRoot(assets); oerr == nil {
				defer root.Close()
				ts = &inspect.TexSource{File: s.Rel(file), Data: data, FS: root.FS()}
			}
		}
		rep, err = inspect.Texture(ir, name, ts, opt)
	case "scene":
		rep, err = inspect.Scene(ir, name, opt)
	case "prefab":
		rep, err = inspect.Prefab(ir, name, opt)
	case "world":
		rep, err = inspect.World(ir, name, opt)
	case "map":
		rep, err = inspect.Map(ir, name, opt)
	}
	if err != nil {
		return nil, err
	}
	for i, p := range rep.Sheets {
		rep.Sheets[i] = s.Rel(p)
	}
	return rep, nil
}

// PartialLibrary loads every asset that compiles, plus the errors of those that do not.
func (s *Session) PartialLibrary() (*asset.Library, asset.Errors, error) {
	return cookLoadPartial(s.Root, s.Project)
}

func init() {
	register(command{
		name:    "inspect",
		usage:   "inspect model|texture|scene|prefab|world|map NAME [--focus ISSUE] [--sheets list]",
		summary: "report (issues ranked by severity, metrics) and sheets for a model, texture, scene, prefab, world or map",
		project: true,
		run: func(env *Env, s *Session, args []string) (any, error) {
			fs := newFlags("inspect", env.Stderr)
			focus := fs.String("focus", "", "keep only issues with this code")
			sheets := fs.String("sheets", "", "comma-separated sheet kinds (default: summary; none; all)")
			var pos []string
			for {
				if err := fs.Parse(args); err != nil {
					return nil, UsageError{err}
				}
				if fs.NArg() == 0 {
					break
				}
				pos = append(pos, fs.Arg(0))
				args = fs.Args()[1:]
			}
			if len(pos) != 2 {
				return nil, usagef("inspect: want KIND NAME, got %v", pos)
			}
			var list []string
			if *sheets != "" {
				list = strings.Split(*sheets, ",")
			}
			rep, err := s.Inspect(pos[0], pos[1], *focus, list)
			if err != nil {
				return nil, err
			}
			return InspectResult{rep}, nil
		},
	})
	prev := mcpExtraTools
	mcpExtraTools = func(m *mcpServer) []mcp.Tool {
		return append(prev(m), mcp.Tool{
			Name:        "inspect",
			Description: "Inspect a model, texture, scene, prefab, world or map: a report with issues ranked by severity (codes like MESH_FLIPPED_NORMALS, TEX_SEAM, SCENE_MISSING_ASSET, PREFAB_FOOTPRINT_SMALL, WORLD_PLACE_OVERLAP, MAP_MISSING_ASSET, each with where it is and a hint naming the source field to change) and metrics, plus the requested sheets as images (default: one summary sheet; a world's is its map around the origin, a map's the whole map with its objects outlined). Read the report first.",
			InputSchema: schema(map[string]any{
				"kind":   enum("asset kind", InspectKinds...),
				"name":   str("asset name"),
				"focus":  str("keep only issues with this code"),
				"sheets": strList("sheet kinds, e.g. [\"normals\"], [\"turntable\"], [\"tiled_2x2\"], [\"on_model:crate\"], [\"ids\"], [\"map\"]; [\"none\"] for the report only"),
			}, "kind", "name"),
			Handler: func(ctx context.Context, args json.RawMessage) (*mcp.Result, error) {
				var a struct {
					Kind   string   `json:"kind"`
					Name   string   `json:"name"`
					Focus  string   `json:"focus"`
					Sheets []string `json:"sheets"`
				}
				if err := mcp.Strict(args, &a); err != nil {
					return errResult(err), nil
				}
				s, err := m.session()
				if err != nil {
					return errResult(err), nil
				}
				rep, err := s.Inspect(a.Kind, a.Name, a.Focus, a.Sheets)
				if err != nil {
					return errResult(err), nil
				}
				var imgs [][]byte
				for _, p := range rep.Sheets {
					imgs = append(imgs, fitPNG(readPNG(s, p), mcpSheetMaxW, mcpSheetMaxH))
				}
				return textResult(rep, imgs...), nil
			},
		})
	}
}
