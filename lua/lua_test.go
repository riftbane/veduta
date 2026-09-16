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
