package script

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/riftbane/veduta/v2/asset"
	"github.com/riftbane/veduta/v2/lua"
)

// Saves: a script stores tables of plain data under a name and reads them back in a later
// run. On disk (or in a scenario) a save is JSON: a sequence is an array, any other table an
// object with string keys, in sorted order; an integer is written as one and a float always
// with a point or an exponent, so reading gives back the same Lua types.

// maxSaveDepth bounds how deeply tables of a save nest (a table that holds itself too).
const maxSaveDepth = 64

func (g *Game) installSave() {
	g.lib("save", map[string]fn{
		"write": func(vm *lua.VM, args []lua.Value) []lua.Value {
			name := saveName(vm, args, "save.write")
			data := vm.CheckTable(args, 1, "save.write")
			var buf bytes.Buffer
			if err := encodeSave(&buf, lua.TableValue(data), "data", 0); err != nil {
				vm.Errorf("save.write: %s", err)
			}
			if buf.Len() > asset.MaxSaveBytes {
				vm.Errorf("save.write: %s is %d bytes, more than a save may hold (%d)", name, buf.Len(), asset.MaxSaveBytes)
			}
			if err := g.ctx.WriteSave(name, buf.Bytes()); err != nil {
				return vm.Ret(lua.Nil, lua.String(err.Error()))
			}
			return vm.Ret(lua.True)
		},
		"read": func(vm *lua.VM, args []lua.Value) []lua.Value {
			name := saveName(vm, args, "save.read")
			b, ok, err := g.ctx.ReadSave(name)
			if err != nil {
				return vm.Ret(lua.Nil, lua.String(err.Error()))
			}
			if !ok {
				return vm.Ret(lua.Nil)
			}
			v, err := decodeSave(b)
			if err != nil {
				return vm.Ret(lua.Nil, lua.String(fmt.Sprintf("save %s: %v", name, err)))
			}
			return vm.Ret(v)
		},
		"remove": func(vm *lua.VM, args []lua.Value) []lua.Value {
			if err := g.ctx.RemoveSave(saveName(vm, args, "save.remove")); err != nil {
				return vm.Ret(lua.Nil, lua.String(err.Error()))
			}
			return vm.Ret(lua.True)
		},
		"list": func(vm *lua.VM, args []lua.Value) []lua.Value {
			names, err := g.ctx.SaveNames()
			if err != nil {
				return vm.Ret(lua.Nil, lua.String(err.Error()))
			}
			t := lua.NewTable(len(names), 0)
			for _, n := range names {
				t.Append(lua.String(n))
			}
			return vm.Ret(lua.TableValue(t))
		},
	})
}

func saveName(vm *lua.VM, args []lua.Value, fname string) string {
	name := vm.CheckString(args, 0, fname)
	if err := asset.ValidName(name); err != nil {
		vm.ArgError(0, fname, "save "+err.Error())
	}
	return name
}

// encodeSave writes v as JSON; path names v in an error.
func encodeSave(w *bytes.Buffer, v lua.Value, path string, depth int) error {
	switch v.Type() {
	case lua.TypeBoolean:
		b, _ := v.Bool()
		w.WriteString(strconv.FormatBool(b))
	case lua.TypeNumber:
		if i, ok := v.Int(); ok && v.IsInt() {
			w.WriteString(strconv.FormatInt(i, 10))
			return nil
		}
		f, _ := v.Float()
		if math.IsInf(f, 0) || math.IsNaN(f) {
			return fmt.Errorf("%s is %v, which a save cannot hold", path, f)
		}
		s := strconv.FormatFloat(f, 'g', -1, 64)
		if !strings.ContainsAny(s, ".eE") {
			s += ".0"
		}
		w.WriteString(s)
	case lua.TypeString:
		s, _ := v.Str()
		if !utf8.ValidString(s) {
			return fmt.Errorf("%s is not UTF-8 text", path)
		}
		b, _ := json.Marshal(s)
		w.Write(b)
	case lua.TypeTable:
		if depth >= maxSaveDepth {
			return fmt.Errorf("%s nests tables more than %d deep (does a table hold itself?)", path, maxSaveDepth)
		}
		t := v.Table()
		n, count := t.Len(), 0
		t.ForEach(func(_, _ lua.Value) bool { count++; return true })
		if n > 0 && n == count {
			w.WriteByte('[')
			for i := 1; i <= n; i++ {
				if i > 1 {
					w.WriteByte(',')
				}
				if err := encodeSave(w, t.GetInt(int64(i)), fmt.Sprintf("%s[%d]", path, i), depth+1); err != nil {
					return err
				}
			}
			w.WriteByte(']')
			return nil
		}
		keys := make([]string, 0, count)
		var bad error
		t.ForEach(func(k, _ lua.Value) bool {
			s, ok := k.Str()
			if !ok || k.Type() != lua.TypeString {
				bad = fmt.Errorf("%s has the key %s: a save's tables are lists or have string keys", path, k.String())
				return false
			}
			keys = append(keys, s)
			return true
		})
		if bad != nil {
			return bad
		}
		sort.Strings(keys)
		w.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				w.WriteByte(',')
			}
			if !utf8.ValidString(k) {
				return fmt.Errorf("%s has a key that is not UTF-8 text", path)
			}
			kb, _ := json.Marshal(k)
			w.Write(kb)
			w.WriteByte(':')
			if err := encodeSave(w, t.GetString(k), path+"."+k, depth+1); err != nil {
				return err
			}
		}
		w.WriteByte('}')
	default:
		return fmt.Errorf("%s is a %s, which a save cannot hold (numbers, strings, booleans and tables of them)", path, v.Type())
	}
	return nil
}

// decodeSave reads a save back into a table, keeping the file's key order and telling
// integers from floats by how they are written.
func decodeSave(b []byte) (lua.Value, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	v, err := decodeValue(dec, 0)
	if err != nil {
		return lua.Nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return lua.Nil, errors.New("data after the save")
	}
	if v.Type() != lua.TypeTable {
		return lua.Nil, errors.New("not an object or an array")
	}
	return v, nil
}

func decodeValue(dec *json.Decoder, depth int) (lua.Value, error) {
	if depth > maxSaveDepth {
		return lua.Nil, fmt.Errorf("nested more than %d deep", maxSaveDepth)
	}
	tok, err := dec.Token()
	if err != nil {
		return lua.Nil, err
	}
	switch x := tok.(type) {
	case json.Delim:
		t := lua.NewTable(0, 0)
		switch x {
		case '[':
			for dec.More() {
				v, err := decodeValue(dec, depth+1)
				if err != nil {
					return lua.Nil, err
				}
				if v.IsNil() {
					return lua.Nil, errors.New("null in an array")
				}
				t.Append(v)
			}
		case '{':
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return lua.Nil, err
				}
				v, err := decodeValue(dec, depth+1)
				if err != nil {
					return lua.Nil, err
				}
				t.SetString(kt.(string), v)
			}
		}
		if _, err := dec.Token(); err != nil { // the closing delimiter
			return lua.Nil, err
		}
		return lua.TableValue(t), nil
	case bool:
		return lua.Bool(x), nil
	case json.Number:
		s := string(x)
		if !strings.ContainsAny(s, ".eE") {
			if i, err := strconv.ParseInt(s, 10, 64); err == nil {
				return lua.Int(i), nil
			}
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return lua.Nil, err
		}
		return lua.Float(f), nil
	case string:
		return lua.String(x), nil
	case nil:
		return lua.Nil, nil
	}
	return lua.Nil, fmt.Errorf("unexpected %v", tok)
}
