// Package fused finds the floating-point multiply-adds that the Go compiler fused into
// single instructions in a linux/arm64 binary.
//
// On arm64 the compiler may turn a*b + c into one FMADD instruction, which rounds once
// where amd64 rounds twice; about a quarter of all inputs then differ in the last bit,
// which is enough to move a pixel or a trace hash. An explicit conversion,
// float32(a*b) + c, is a rounding point the compiler may not fuse across (the Go
// specification says so), so every fused instruction marks a source line where that
// conversion is missing. Scan lists those lines, which turns a determinism rule into
// something a test or `veduta doctor` can check instead of something an author must
// remember.
//
// Scan works on any linux/arm64 Go binary, but its file names are only comparable across
// machines when the binary was built with -trimpath: the compiler then records a file of
// the main module as <module path>/<dir>/<file>, a file of a module from the cache as
// <module path>@<version>/..., and a standard library file as <package>/<file>, whatever
// directory, symlink or GOFLAGS the build ran with. InModule matches those names.
package fused

import (
	"debug/elf"
	"debug/gosym"
	"encoding/binary"
	"fmt"
	"sort"
	"strings"
)

// Site is one source line that compiled to fused instructions inside one function.
type Site struct {
	File string `json:"file"` // path as the compiler recorded it
	Line int    `json:"line"`
	// Func is the function the instructions belong to in the binary. When the line was
	// inlined, that is the caller, so a line of an engine helper inlined into a game
	// function has the helper's File and the game's Func.
	Func    string `json:"func"`
	Package string `json:"package"` // import path of Func's package ("main" for a command)
	Count   int    `json:"count"`   // fused instructions attributed to the line in Func
}

// String formats the site as file:line (func).
func (s Site) String() string { return fmt.Sprintf("%s:%d (%s)", s.File, s.Line, s.Func) }

// Scan reads the linux/arm64 ELF binary at path and returns the source lines whose fused
// instructions keep accepts (nil keeps every one), one Site per file, line and function,
// sorted by file, line and function. keep sees every field but Count. The position of
// inlined code is the inlined source line, as in a stack trace, so a product rounded in a
// small helper is attributed to the helper's file and to the function it was inlined
// into.
func Scan(path string, keep func(Site) bool) ([]Site, error) {
	t, err := open(path)
	if err != nil {
		return nil, err
	}
	type key struct {
		file string
		line int
		fn   string
	}
	sites := map[key]*Site{} // nil for a key keep refused
	for off := 0; off+4 <= len(t.code); off += 4 {
		if !IsFused(binary.LittleEndian.Uint32(t.code[off:])) {
			continue
		}
		file, line, fn := t.syms.PCToLine(t.addr + uint64(off))
		if fn == nil || file == "" {
			continue
		}
		k := key{file, line, fn.Name}
		if s, seen := sites[k]; seen {
			if s != nil {
				s.Count++
			}
			continue
		}
		s := Site{File: file, Line: line, Func: fn.Name, Package: packageOf(fn)}
		if keep != nil && !keep(s) {
			sites[k] = nil
			continue
		}
		s.Count = 1
		sites[k] = &s
	}
	out := make([]Site, 0, len(sites))
	for _, s := range sites {
		if s != nil {
			out = append(out, *s)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Func < b.Func
	})
	return out, nil
}

// InModule reports whether name, a file or package path as a -trimpath build records it,
// belongs to the module whose path is module: it is the module's root package or lies
// under it. A module whose path merely starts with the same letters (demolition for demo)
// does not match, nor does a copy of the module from the module cache, which is recorded
// with its version (demo@v1.2.0/...).
func InModule(name, module string) bool {
	return module != "" && (name == module || strings.HasPrefix(name, module+"/"))
}

// packageOf returns the import path of fn's package. The linker writes a dot in the last
// element of an import path as %2e (gopkg.in/yaml%2ev3.Unmarshal), which is undone here.
func packageOf(fn *gosym.Func) string {
	return strings.ReplaceAll(fn.PackageName(), "%2e", ".")
}

// table is the text and line table of a binary.
type table struct {
	code []byte
	addr uint64
	syms *gosym.Table
}

func open(path string) (*table, error) {
	f, err := elf.Open(path)
	if err != nil {
		return nil, fmt.Errorf("fused: %w", err)
	}
	defer f.Close()
	if f.Machine != elf.EM_AARCH64 {
		return nil, fmt.Errorf("fused: %s is a %v binary, not arm64", path, f.Machine)
	}
	text, pcln := f.Section(".text"), f.Section(".gopclntab")
	if text == nil || pcln == nil {
		return nil, fmt.Errorf("fused: %s has no Go text or line table", path)
	}
	code, err := text.Data()
	if err != nil {
		return nil, fmt.Errorf("fused: read text of %s: %w", path, err)
	}
	tab, err := pcln.Data()
	if err != nil {
		return nil, fmt.Errorf("fused: read line table of %s: %w", path, err)
	}
	st, err := gosym.NewTable(nil, gosym.NewLineTable(tab, text.Addr))
	if err != nil {
		return nil, fmt.Errorf("fused: line table of %s: %w", path, err)
	}
	return &table{code: code, addr: text.Addr, syms: st}, nil
}

// IsFused reports whether the A64 instruction word w is one of the fused
// floating-point operations FMADD, FMSUB, FNMADD or FNMSUB, at any precision. They are
// exactly the "floating-point data-processing (3 source)" group: M=0, S=0, bits 28-24
// set.
func IsFused(w uint32) bool { return w&0xff000000 == 0x1f000000 }
