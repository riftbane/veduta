package lua

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runFile runs a script from testdata in a fresh VM and returns its results.
func runFile(t *testing.T, name string) []Value {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	vm := New(Options{Stdout: &out})
	openTestLibs(vm)
	vm.SetGlobal("VEDUTA", True)
	f, err := vm.Load(name, string(src))
	if err != nil {
		t.Fatal(err)
	}
	res, err := vm.Call(FunctionValue(f))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return res
}

// openTestLibs loads the libraries a test script may use: the ones this package provides.
var openTestLibs = func(vm *VM) {}

func TestCoreScript(t *testing.T) {
	if res := runFile(t, "core.lua"); len(res) != 1 || res[0] != String("ok") {
		t.Fatalf("core.lua returned %v", res)
	}
}

// TestLibsMatchReferenceLua runs testdata/libs.lua and compares what it prints, line by line,
// with testdata/libs.out, the output of PUC Lua 5.4.6 (lua5.4 libs.lua > libs.out).
func TestLibsMatchReferenceLua(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("testdata", "libs.lua"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "libs.out"))
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	vm := New(Options{Stdout: &out})
	f, err := vm.Load("libs.lua", string(src))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := vm.Call(FunctionValue(f)); err != nil {
		t.Fatalf("libs.lua: %v\noutput so far:\n%s", err, out.String())
	}
	got, wantLines := strings.Split(out.String(), "\n"), strings.Split(string(want), "\n")
	for i := 0; i < max(len(got), len(wantLines)); i++ {
		g, w := "", ""
		if i < len(got) {
			g = got[i]
		}
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if g != w {
			t.Errorf("line %d:\n got %q\nwant %q", i+1, g, w)
		}
	}
}
