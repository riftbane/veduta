package fused

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// engineModule is the module path of the engine, as a -trimpath build records its files.
const engineModule = "github.com/riftbane/veduta"

func TestIsFused(t *testing.T) {
	for _, c := range []struct {
		w    uint32
		want bool
		what string
	}{
		{0x1f4120a8, true, "FMADDD"},
		{0x1f0c356b, true, "FMADDS"},
		{0x1f2cb570, true, "FNMSUBS"},
		{0x1f608821, true, "FNMSUBD"},
		{0x1e29096b, false, "FMULS"},
		{0x1e2c100c, false, "FMOVS $0.5"},
		{0x1e331929, false, "FDIVS"},
		{0x1e214129, false, "FNEGS"},
		{0x9104b3fb, false, "ADD $300, RSP"},
		{0xd503201f, false, "NOP"},
	} {
		if got := IsFused(c.w); got != c.want {
			t.Errorf("IsFused(%#08x) [%s] = %v, want %v", c.w, c.what, got, c.want)
		}
	}
}

func TestInModule(t *testing.T) {
	for _, c := range []struct {
		name, module string
		want         bool
	}{
		{"demo/game/kinds.go", "demo", true},
		{"demo", "demo", true},
		{"demo/game", "demo", true},
		{"demolition/game/kinds.go", "demo", false},
		{"demo@v1.2.0/game/kinds.go", "demo", false},
		{"github.com/riftbane/veduta/gmath/vec.go", engineModule, true},
		{"github.com/riftbane/veduta@v1.0.0/gmath/vec.go", engineModule, false},
		{"/home/me/veduta/gmath/vec.go", engineModule, false},
		{"runtime/proc.go", "", false},
	} {
		if got := InModule(c.name, c.module); got != c.want {
			t.Errorf("InModule(%q, %q) = %v, want %v", c.name, c.module, got, c.want)
		}
	}
}

// goTool returns the go command, or skips when there is none.
func goTool(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("builds arm64 binaries")
	}
	p, err := exec.LookPath("go")
	if err != nil {
		p = filepath.Join(runtime.GOROOT(), "bin", "go")
		if _, err := os.Stat(p); err != nil {
			t.Skip("no go command")
		}
	}
	return p
}

// buildArm64 builds pkg for the console the way a release does: -trimpath, so that file
// names are module paths whatever directory or symlink it is reached through, and without
// the caller's GOFLAGS, which could turn -trimpath off or change the code generated.
func buildArm64(t *testing.T, gocmd, dir, out, pkg string, flags ...string) {
	t.Helper()
	args := append([]string{"build", "-trimpath"}, flags...)
	cmd := exec.Command(gocmd, append(args, "-o", out, pkg)...)
	cmd.Dir = dir
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GOFLAGS=") {
			env = append(env, kv)
		}
	}
	cmd.Env = append(env, "GOOS=linux", "GOARCH=arm64", "CGO_ENABLED=0")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build %s for linux/arm64: %v\n%s", pkg, err, b)
	}
}

// inModule is the filter of the engine gate, and of the control that proves it can match.
func inModule(module string) func(Site) bool {
	return func(s Site) bool { return InModule(s.File, module) }
}

// linesIn reports whether the binary's line table holds any file of module: a filter that
// matches none of them would find no fused site and pass whatever the code does.
func linesIn(t *testing.T, bin, module string) bool {
	t.Helper()
	files, err := Files(bin)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if InModule(f, module) {
			return true
		}
	}
	return false
}

// TestScanFindsAKnownFusion is the control for the engine check below: a scanner or a
// filter that silently found nothing would make that check pass forever. It builds a
// probe module the way the gate builds the engine, through a symlink and with GOFLAGS
// asking for full paths, and filters with the same module-path match.
func TestScanFindsAKnownFusion(t *testing.T) {
	gocmd := goTool(t)
	probe := t.TempDir()
	src := `package main

import "os"

//go:noinline
func bare(a, b, c float32) float32 {
	return a*b + c
}

//go:noinline
func rounded(a, b, c float32) float32 {
	return float32(a*b) + c
}

func helper(a, b, c float32) float32 {
	return a*b + c
}

//go:noinline
func callerA(a, b, c float32) float32 { return helper(a, b, c) }

//go:noinline
func callerB(a, b, c float32) float32 { return helper(c, b, a) }

func main() {
	os.Exit(int(bare(1, 2, 3) + rounded(1, 2, 3) + callerA(1, 2, 3) + callerB(1, 2, 3)))
}
`
	if err := os.WriteFile(filepath.Join(probe, "main.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(probe, "go.mod"), []byte("module example.com/probe\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := probe
	if link := filepath.Join(t.TempDir(), "link"); os.Symlink(probe, link) == nil {
		dir = link
	}
	t.Setenv("GOFLAGS", "-trimpath=false")
	bin := filepath.Join(t.TempDir(), "probe")
	buildArm64(t, gocmd, dir, bin, ".")
	if !linesIn(t, bin, "example.com/probe") {
		t.Fatal("no file of example.com/probe in the line table: the module filter is blind")
	}
	sites, err := Scan(bin, inModule("example.com/probe"))
	if err != nil {
		t.Fatal(err)
	}
	// The helper's line is listed once per function it was inlined into, and the package
	// of those functions is known, so a caller can tell whose code holds the fusion.
	var got []string
	for _, s := range sites {
		got = append(got, s.String()+" in "+s.Package)
	}
	want := []string{
		"example.com/probe/main.go:7 (main.bare) in main",
		"example.com/probe/main.go:16 (main.callerA) in main",
		"example.com/probe/main.go:16 (main.callerB) in main",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("sites:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if _, err := Scan(filepath.Join(probe, "main.go"), nil); err == nil {
		t.Fatal("Scan of a source file succeeded")
	}
}

// TestEngineHasNoFusedMultiplyAdd is the gate that keeps the engine deterministic on
// arm64, the console's architecture: no line of the engine may compile to a fused
// multiply-add. It scans the veduta tool and the engine's test game, which between them link
// every package that draws, simulates, cooks or inspects, and a program that refers to
// every exported function and method of the public engine packages, so that API no linked
// binary happens to call (the linker drops it) is scanned too.
func TestEngineHasNoFusedMultiplyAdd(t *testing.T) {
	gocmd := goTool(t)
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	// The overlay names a file by its physical path, which is where the go command finds
	// the module when the checkout is reached through a symlink.
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Skip("not inside the engine repository")
	}
	dir := t.TempDir()
	for _, pkg := range []string{"./cmd/veduta", "./internal/testgame/cmd/game"} {
		bin := filepath.Join(dir, filepath.Base(pkg))
		buildArm64(t, gocmd, root, bin, pkg)
		scanEngine(t, pkg, bin)
	}

	src, refs, skipped := apiProgram(t, gocmd, root)
	for _, s := range skipped {
		t.Logf("exported API not scanned (generic, no instantiation to refer to): %s", s)
	}
	if refs < 100 {
		t.Fatalf("the API program refers to %d functions and methods; the package list or the parser is broken", refs)
	}
	// The program is laid over the module with -overlay, so it can import the engine as a
	// package of the module without a file being written into the checkout.
	gen := filepath.Join(dir, "apiref.go")
	if err := os.WriteFile(gen, src, 0o644); err != nil {
		t.Fatal(err)
	}
	overlay := filepath.Join(dir, "overlay.json")
	target := filepath.Join(root, "internal", "fused", "apiref", "main.go")
	ov, _ := json.Marshal(map[string]map[string]string{"Replace": {target: gen}})
	if err := os.WriteFile(overlay, ov, 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "apiref")
	buildArm64(t, gocmd, root, bin, "./internal/fused/apiref", "-overlay", overlay)
	scanEngine(t, "exported API", bin)
}

// apiProgram writes a main package that refers to every exported top-level function and
// every exported method of an exported type in the engine's public packages (neither
// internal nor main) as they build for linux/arm64, and returns its source, the number of
// references and the generic declarations it had to leave out. Every exported type also
// goes into an interface, and main looks a method up by a name known only at run time,
// which makes the linker keep the exported methods of every type in the program, the
// promoted ones included.
func apiProgram(t *testing.T, gocmd, root string) ([]byte, int, []string) {
	t.Helper()
	cmd := exec.Command(gocmd, "list", "-f", "{{.ImportPath}}\t{{.Name}}\t{{.Dir}}\t{{join .GoFiles \",\"}}", "./...")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOFLAGS=", "GOOS=linux", "GOARCH=arm64", "CGO_ENABLED=0")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	var imports, refs, skipped []string
	fset := token.NewFileSet()
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 4 || f[1] == "main" || f[3] == "" {
			continue
		}
		path, dir := f[0], f[2]
		if rel := strings.TrimPrefix(path, engineModule); strings.Contains(rel+"/", "/internal/") {
			continue
		}
		alias, before := fmt.Sprintf("p%d", len(imports)), len(refs)
		for _, name := range strings.Split(f[3], ",") {
			file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.SkipObjectResolution)
			if err != nil {
				t.Fatal(err)
			}
			for _, decl := range file.Decls {
				switch d := decl.(type) {
				case *ast.FuncDecl:
					if !d.Name.IsExported() {
						continue
					}
					where := path + "." + d.Name.Name
					if d.Recv == nil {
						if d.Type.TypeParams != nil {
							skipped = append(skipped, where)
							continue
						}
						refs = append(refs, alias+"."+d.Name.Name)
						continue
					}
					typ, generic := receiver(d.Recv.List[0].Type)
					switch {
					case !ast.IsExported(typ):
					case generic:
						skipped = append(skipped, path+"."+typ+"."+d.Name.Name)
					default:
						refs = append(refs, fmt.Sprintf("(*%s.%s).%s", alias, typ, d.Name.Name))
					}
				case *ast.GenDecl:
					for _, spec := range d.Specs {
						if ts, ok := spec.(*ast.TypeSpec); ok && ts.Name.IsExported() && ts.TypeParams == nil {
							refs = append(refs, fmt.Sprintf("(*%s.%s)(nil)", alias, ts.Name.Name))
						}
					}
				}
			}
		}
		if len(refs) == before {
			alias = "_" // nothing exported to refer to, such as a package of embedded files
		}
		imports = append(imports, fmt.Sprintf("%s %q", alias, path))
	}
	var b strings.Builder
	b.WriteString("// Code generated by TestEngineHasNoFusedMultiplyAdd. DO NOT EDIT.\n\npackage main\n\nimport (\n\t\"os\"\n\t\"reflect\"\n\n")
	for _, imp := range imports {
		fmt.Fprintf(&b, "\t%s\n", imp)
	}
	b.WriteString(")\n\nvar sink = []any{\n")
	for _, r := range refs {
		fmt.Fprintf(&b, "\t%s,\n", r)
	}
	b.WriteString("}\n\nfunc main() {\n\tfor _, v := range sink {\n\t\t_ = reflect.ValueOf(v).MethodByName(os.Args[0])\n\t}\n}\n")
	src, err := format.Source([]byte(b.String()))
	if err != nil {
		t.Fatalf("generated API program: %v\n%s", err, b.String())
	}
	return src, len(refs), skipped
}

// receiver returns the name of a method's receiver type and whether that type is generic.
func receiver(e ast.Expr) (string, bool) {
	switch x := e.(type) {
	case *ast.StarExpr:
		return receiver(x.X)
	case *ast.ParenExpr:
		return receiver(x.X)
	case *ast.Ident:
		return x.Name, false
	case *ast.IndexExpr:
		name, _ := receiver(x.X)
		return name, true
	case *ast.IndexListExpr:
		name, _ := receiver(x.X)
		return name, true
	}
	return "", false
}

// scanEngine fails the test for every engine line that fused in bin.
func scanEngine(t *testing.T, what, bin string) {
	t.Helper()
	if !linesIn(t, bin, engineModule) {
		t.Fatalf("%s: no engine file in the line table, so the check cannot see the engine", what)
	}
	sites, err := Scan(bin, inModule(engineModule))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sites {
		t.Errorf("%s: %s:%d in %s compiles to %d fused multiply-add(s) on arm64; round the product with an explicit conversion, e.g. float32(a*b) + c",
			what, strings.TrimPrefix(s.File, engineModule+"/"), s.Line, s.Func, s.Count)
	}
}
