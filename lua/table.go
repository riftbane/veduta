package lua

import "math"

// Table is a Lua table: an array part for the keys 1…n and a hash part that remembers the
// order its keys were first set in, so that pairs and next visit a table in the same order
// on every run and every machine.
type Table struct {
	arr []Value // t[1] … t[len(arr)]; the last element is never nil

	keys  []Value          // hash part, in insertion order; a removed entry keeps its key with a nil value
	vals  []Value          // values of keys
	strs  map[string]int32 // string key → slot
	other map[Value]int32  // any other key → slot
	live  int              // non-nil values in the hash part

	meta *Table
}

// NewTable returns a table with room for narr array elements and nhash other keys.
func NewTable(narr, nhash int) *Table {
	t := &Table{}
	if narr > 0 {
		t.arr = make([]Value, 0, narr)
	}
	if nhash > 0 {
		t.keys = make([]Value, 0, nhash)
		t.vals = make([]Value, 0, nhash)
	}
	return t
}

// Metatable returns the table's metatable, or nil.
func (t *Table) Metatable() *Table { return t.meta }

// SetMetatable sets the table's metatable (nil removes it).
func (t *Table) SetMetatable(m *Table) { t.meta = m }

// Len returns the length of the table's sequence: the border # reports.
func (t *Table) Len() int { return len(t.arr) }

// Get returns t[k] without metamethods.
func (t *Table) Get(k Value) Value {
	switch k.k {
	case kindInt:
		return t.GetInt(k.i())
	case kindString:
		return t.GetString(k.s())
	case kindFloat:
		if i, ok := floatToInt(k.f()); ok {
			return t.GetInt(i)
		}
	case kindNil:
		return Nil
	}
	if slot, ok := t.other[k]; ok {
		return t.vals[slot]
	}
	return Nil
}

// GetInt returns t[i] without metamethods.
func (t *Table) GetInt(i int64) Value {
	if uint64(i-1) < uint64(len(t.arr)) {
		return t.arr[i-1]
	}
	if t.other == nil {
		return Nil
	}
	if slot, ok := t.other[Int(i)]; ok {
		return t.vals[slot]
	}
	return Nil
}

// GetString returns t[s] without metamethods.
func (t *Table) GetString(s string) Value {
	if slot, ok := t.strs[s]; ok {
		return t.vals[slot]
	}
	return Nil
}

// keyError reports why k cannot be a key, or "" when it can.
func keyError(k Value) string {
	switch {
	case k.k == kindNil:
		return "index is nil"
	case k.k == kindFloat && math.IsNaN(k.f()):
		return "index is NaN"
	}
	return ""
}

// Set sets t[k] = v without metamethods. It panics with an error value for a nil or NaN key;
// VM.SetTable reports those as Lua errors.
func (t *Table) Set(k, v Value) {
	switch k.k {
	case kindInt:
		t.SetInt(k.i(), v)
		return
	case kindString:
		t.SetString(k.s(), v)
		return
	case kindFloat:
		if i, ok := floatToInt(k.f()); ok {
			t.SetInt(i, v)
			return
		}
	}
	if msg := keyError(k); msg != "" {
		panic(&Error{Value: String(msg)})
	}
	if t.other == nil {
		t.other = map[Value]int32{}
	}
	if slot, ok := t.other[k]; ok {
		t.setSlot(slot, v)
		return
	}
	if !v.IsNil() {
		t.other[k] = t.addSlot(k, v)
	}
}

// SetInt sets t[i] = v without metamethods.
func (t *Table) SetInt(i int64, v Value) {
	n := int64(len(t.arr))
	switch {
	case i >= 1 && i <= n:
		t.arr[i-1] = v
		if i == n && v.IsNil() {
			t.trim()
		}
		return
	case i == n+1 && !v.IsNil():
		if t.other != nil {
			if slot, ok := t.other[Int(i)]; ok {
				t.removeSlot(slot)
				delete(t.other, Int(i))
			}
		}
		t.arr = append(t.arr, v)
		t.migrate()
		return
	}
	k := Int(i)
	if t.other == nil {
		if v.IsNil() {
			return
		}
		t.other = map[Value]int32{}
	}
	if slot, ok := t.other[k]; ok {
		t.setSlot(slot, v)
		return
	}
	if !v.IsNil() {
		t.other[k] = t.addSlot(k, v)
	}
}

// SetString sets t[s] = v without metamethods.
func (t *Table) SetString(s string, v Value) {
	if slot, ok := t.strs[s]; ok {
		t.setSlot(slot, v)
		return
	}
	if v.IsNil() {
		return
	}
	if t.strs == nil {
		t.strs = map[string]int32{}
	}
	t.strs[s] = t.addSlot(String(s), v)
}

// Append sets t[#t+1] = v.
func (t *Table) Append(v Value) { t.SetInt(int64(len(t.arr))+1, v) }

// trim drops the nils at the end of the array part.
func (t *Table) trim() {
	n := len(t.arr)
	for n > 0 && t.arr[n-1].IsNil() {
		n--
	}
	clear(t.arr[n:])
	t.arr = t.arr[:n]
}

// migrate moves the integer keys that follow the array part out of the hash part.
func (t *Table) migrate() {
	for t.other != nil {
		k := Int(int64(len(t.arr)) + 1)
		slot, ok := t.other[k]
		if !ok || t.vals[slot].IsNil() {
			return
		}
		t.arr = append(t.arr, t.vals[slot])
		t.removeSlot(slot)
		delete(t.other, k)
	}
}

func (t *Table) setSlot(slot int32, v Value) {
	was := !t.vals[slot].IsNil()
	t.vals[slot] = v
	switch {
	case was && v.IsNil():
		t.live--
	case !was && !v.IsNil():
		t.live++
	}
}

// removeSlot empties a slot whose key leaves the hash part for good.
func (t *Table) removeSlot(slot int32) {
	t.setSlot(slot, Nil)
}

// addSlot appends a key, compacting the hash part first when most of it is removed entries.
// A new key may be added only outside a traversal (as in Lua), so the slots next is walking
// can move.
func (t *Table) addSlot(k, v Value) int32 {
	if len(t.keys) >= 8 && t.live < len(t.keys)/2 {
		t.compact()
	}
	t.keys = append(t.keys, k)
	t.vals = append(t.vals, v)
	t.live++
	return int32(len(t.keys) - 1)
}

// compact removes the entries whose value is nil and renumbers the rest.
func (t *Table) compact() {
	n := 0
	for i, k := range t.keys {
		v := t.vals[i]
		if k.k == kindString {
			if v.IsNil() {
				delete(t.strs, k.s())
				continue
			}
			t.strs[k.s()] = int32(n)
		} else {
			if v.IsNil() {
				delete(t.other, k)
				continue
			}
			t.other[k] = int32(n)
		}
		t.keys[n], t.vals[n] = k, v
		n++
	}
	clear(t.keys[n:])
	clear(t.vals[n:])
	t.keys, t.vals = t.keys[:n], t.vals[:n]
}

// Next returns the key and value that follow k in a traversal (k nil starts one), and false
// when there are no more. It reports an error for a key the table does not have.
func (t *Table) Next(k Value) (Value, Value, bool, error) {
	i := 0 // position to look from: array indices first, then hash slots after them
	switch {
	case k.IsNil():
	default:
		k = normKey(k)
		if k.k == kindInt && uint64(k.i()-1) < uint64(len(t.arr)) {
			i = int(k.i())
			break
		}
		slot, ok := int32(0), false
		if k.k == kindString {
			slot, ok = t.strs[k.s()]
		} else if t.other != nil {
			slot, ok = t.other[k]
		}
		switch {
		case ok:
			i = len(t.arr) + int(slot) + 1
		case k.k == kindInt && k.i() > int64(len(t.arr)):
			// An index the array part had when the traversal passed it, before the
			// traversal set it and the ones after it to nil: carry on with the hash part.
			i = len(t.arr)
		default:
			return Nil, Nil, false, &Error{Value: String("invalid key to 'next'")}
		}
	}
	for ; i < len(t.arr); i++ {
		if v := t.arr[i]; !v.IsNil() {
			return Int(int64(i) + 1), v, true, nil
		}
	}
	for slot := i - len(t.arr); slot < len(t.keys); slot++ {
		if v := t.vals[slot]; !v.IsNil() {
			return t.keys[slot], v, true, nil
		}
	}
	return Nil, Nil, false, nil
}

// ForEach calls f for every key and value in traversal order until f returns false.
func (t *Table) ForEach(f func(k, v Value) bool) {
	for i, v := range t.arr {
		if !v.IsNil() && !f(Int(int64(i)+1), v) {
			return
		}
	}
	for slot, k := range t.keys {
		if v := t.vals[slot]; !v.IsNil() && !f(k, v) {
			return
		}
	}
}
