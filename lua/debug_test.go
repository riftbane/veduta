package lua

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

type recorder struct {
	lines []string
	at    func(vm *VM, line int)
}

func (r *recorder) Statement(vm *VM, chunk string, line, depth int) {
	r.lines = append(r.lines, fmt.Sprintf("%s:%d@%d", chunk, line, depth))
	if r.at != nil {
		r.at(vm, line)
	}
}

func names(vars []Variable) string {
	var s []string
	for _, v := range vars {
		s = append(s, v.Name+"="+v.Value.String())
	}
	return strings.Join(s, " ")
}

// TestDebugger: a VM with a debugger is told of every statement with its call depth, and
// while stopped shows the stack, the locals in scope and the upvalues.
func TestDebugger(t *testing.T) {
	src := `local a = 1
local function f(x)
  local y = x + a
  return y
end
local r = f(2)
for i = 1, 2 do
  local z = i
end
`
	checked := 0
	rec := &recorder{}
	rec.at = func(vm *VM, line int) {
		switch line {
		case 4:
			if got := names(vm.Locals(0)); got != "x=2 y=3" {
				t.Errorf("locals at line 4: %s", got)
			}
			if got := names(vm.Upvalues(0)); got != "a=1" {
				t.Errorf("upvalues at line 4: %s", got)
			}
			if got := names(vm.Locals(1)); got != "a=1 f=function" && !strings.HasPrefix(got, "a=1 f=function") {
				t.Errorf("the caller's locals at line 4: %s", got)
			}
			want := []StackEntry{{Chunk: "t.lua", Name: "local function 'f'", Line: 4}, {Chunk: "t.lua", Name: "main chunk", Line: 6}}
			if got := vm.Stack(); len(got) != 2 || got[0] != want[0] || got[1].Line != 6 {
				t.Errorf("stack at line 4: %+v", got)
			}
			checked++
		case 8:
			if got := names(vm.Locals(0)); !strings.HasPrefix(got, "a=1 f=") || !strings.HasSuffix(got, " r=3 i=1") && !strings.HasSuffix(got, " r=3 i=2") {
				t.Errorf("locals at line 8: %s", got)
			}
			checked++
		}
	}
	vm := New(Options{Debugger: rec})
	fn, err := vm.Load("t.lua", src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := vm.Call(FunctionValue(fn)); err != nil {
		t.Fatal(err)
	}
	want := []string{"t.lua:1@1", "t.lua:2@1", "t.lua:6@1", "t.lua:3@2", "t.lua:4@2", "t.lua:7@1", "t.lua:8@1", "t.lua:8@1"}
	if !reflect.DeepEqual(rec.lines, want) {
		t.Errorf("statements %v, want %v", rec.lines, want)
	}
	if checked != 3 {
		t.Errorf("stopped at the checked lines %d times, want 3", checked)
	}

	plain := New(Options{})
	fn, _ = plain.Load("t.lua", src)
	if _, err := plain.Call(FunctionValue(fn)); err != nil || plain.Locals(0) != nil {
		t.Errorf("a VM without a debugger: %v %v", err, plain.Locals(0))
	}
}
