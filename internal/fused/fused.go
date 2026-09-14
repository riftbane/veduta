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
package fused

import (
	"debug/elf"
	"debug/gosym"
	"encoding/binary"
	"fmt"
	"sort"
)

// Site is one source line that compiled to fused instructions.
type Site struct {
	File  string `json:"file"` // path as the compiler recorded it
	Line  int    `json:"line"`
	Func  string `json:"func"`  // the function holding the code (the caller when inlined)
	Count int    `json:"count"` // fused instructions attributed to the line
}

// String formats the site as file:line (func).
func (s Site) String() string { return fmt.Sprintf("%s:%d (%s)", s.File, s.Line, s.Func) }

// Scan reads the linux/arm64 ELF binary at path and returns the source lines whose fused
// instructions come from a file that keep accepts (nil keeps every file), sorted by file
// and line. The position of inlined code is the inlined source line, as in a stack
// trace, so a product rounded in a small helper is attributed to the helper.
func Scan(path string, keep func(file string) bool) ([]Site, error) {
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
	type key struct {
		file string
		line int
	}
	sites := map[key]*Site{}
	for off := 0; off+4 <= len(code); off += 4 {
		if !IsFused(binary.LittleEndian.Uint32(code[off:])) {
			continue
		}
		file, line, fn := st.PCToLine(text.Addr + uint64(off))
		if fn == nil || file == "" || (keep != nil && !keep(file)) {
			continue
		}
		k := key{file, line}
		if s := sites[k]; s != nil {
			s.Count++
			continue
		}
		sites[k] = &Site{File: file, Line: line, Func: fn.Name, Count: 1}
	}
	out := make([]Site, 0, len(sites))
	for _, s := range sites {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out, nil
}

// IsFused reports whether the A64 instruction word w is one of the fused
// floating-point operations FMADD, FMSUB, FNMADD or FNMSUB, at any precision. They are
// exactly the "floating-point data-processing (3 source)" group: M=0, S=0, bits 28-24
// set.
func IsFused(w uint32) bool { return w&0xff000000 == 0x1f000000 }
