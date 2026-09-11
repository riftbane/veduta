package sim

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"

	"github.com/riftbane/veduta/gmath"
)

// AppendCanonical appends the canonical JSON encoding of v to dst. Canonical JSON is
// what the trace is written and hashed in:
//
//   - object keys are sorted (byte order), no insignificant whitespace;
//   - float32 values use the shortest decimal that round-trips as float32, float64 values
//     the shortest that round-trips as float64, in encoding/json's notation (exponent form
//     below 1e-6 and from 1e21);
//   - non-finite numbers, which JSON cannot represent, are the strings "NaN", "+Inf" and
//     "-Inf" so a broken simulation can still be traced;
//   - vectors are arrays; structs are encoded through their JSON form (json tags apply),
//     then canonicalized.
//
// Supported: nil, bool, string, all integer and float kinds, gmath.Vec2/Vec3/Vec4/Quat,
// slices and arrays, maps with string keys, json.Number, and structs or pointers that
// encoding/json accepts.
func AppendCanonical(dst []byte, v any) ([]byte, error) {
	switch x := v.(type) {
	case nil:
		return append(dst, "null"...), nil
	case bool:
		return strconv.AppendBool(dst, x), nil
	case string:
		return appendString(dst, x), nil
	case int:
		return strconv.AppendInt(dst, int64(x), 10), nil
	case int32:
		return strconv.AppendInt(dst, int64(x), 10), nil
	case int64:
		return strconv.AppendInt(dst, x, 10), nil
	case uint32:
		return strconv.AppendUint(dst, uint64(x), 10), nil
	case uint64:
		return strconv.AppendUint(dst, x, 10), nil
	case float32:
		return appendFloat(dst, float64(x), 32), nil
	case float64:
		return appendFloat(dst, x, 64), nil
	case json.Number:
		return append(dst, x...), nil
	case gmath.Vec2:
		return appendFloats(dst, x.X, x.Y), nil
	case gmath.Vec3:
		return appendFloats(dst, x.X, x.Y, x.Z), nil
	case gmath.Vec4:
		return appendFloats(dst, x.X, x.Y, x.Z, x.W), nil
	case gmath.Quat:
		return appendFloats(dst, x.X, x.Y, x.Z, x.W), nil
	case []any:
		dst = append(dst, '[')
		for i, e := range x {
			if i > 0 {
				dst = append(dst, ',')
			}
			var err error
			if dst, err = AppendCanonical(dst, e); err != nil {
				return dst, err
			}
		}
		return append(dst, ']'), nil
	case []string:
		dst = append(dst, '[')
		for i, e := range x {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = appendString(dst, e)
		}
		return append(dst, ']'), nil
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		dst = append(dst, '{')
		for i, k := range keys {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = appendString(dst, k)
			dst = append(dst, ':')
			var err error
			if dst, err = AppendCanonical(dst, x[k]); err != nil {
				return dst, err
			}
		}
		return append(dst, '}'), nil
	}
	return appendReflect(dst, reflect.ValueOf(v))
}

func appendReflect(dst []byte, rv reflect.Value) ([]byte, error) {
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.AppendInt(dst, rv.Int(), 10), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return strconv.AppendUint(dst, rv.Uint(), 10), nil
	case reflect.Float32:
		return appendFloat(dst, rv.Float(), 32), nil
	case reflect.Float64:
		return appendFloat(dst, rv.Float(), 64), nil
	case reflect.Bool:
		return strconv.AppendBool(dst, rv.Bool()), nil
	case reflect.String:
		return appendString(dst, rv.String()), nil
	case reflect.Slice, reflect.Array:
		if rv.Kind() == reflect.Slice && rv.IsNil() {
			return append(dst, "null"...), nil
		}
		dst = append(dst, '[')
		for i := 0; i < rv.Len(); i++ {
			if i > 0 {
				dst = append(dst, ',')
			}
			var err error
			if dst, err = AppendCanonical(dst, rv.Index(i).Interface()); err != nil {
				return dst, err
			}
		}
		return append(dst, ']'), nil
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			return dst, fmt.Errorf("sim: canonical JSON: map key type %s is not a string", rv.Type().Key())
		}
		m := make(map[string]any, rv.Len())
		it := rv.MapRange()
		for it.Next() {
			m[it.Key().String()] = it.Value().Interface()
		}
		return AppendCanonical(dst, m)
	case reflect.Pointer, reflect.Interface:
		if rv.IsNil() {
			return append(dst, "null"...), nil
		}
		return AppendCanonical(dst, rv.Elem().Interface())
	case reflect.Struct:
		// Go through encoding/json so json tags and custom marshalers apply, then
		// canonicalize the result (float formatting and key order).
		raw, err := json.Marshal(rv.Interface())
		if err != nil {
			return dst, fmt.Errorf("sim: canonical JSON: %w", err)
		}
		var generic any
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if err := dec.Decode(&generic); err != nil {
			return dst, err
		}
		return AppendCanonical(dst, generic)
	}
	return dst, fmt.Errorf("sim: canonical JSON: unsupported type %s", rv.Type())
}

func appendFloats(dst []byte, fs ...float32) []byte {
	dst = append(dst, '[')
	for i, f := range fs {
		if i > 0 {
			dst = append(dst, ',')
		}
		dst = appendFloat(dst, float64(f), 32)
	}
	return append(dst, ']')
}

// appendFloat formats like encoding/json, with the shortest representation for the
// given bit size, and strings for non-finite values.
func appendFloat(dst []byte, f float64, bitSize int) []byte {
	switch {
	case math.IsNaN(f):
		return append(dst, `"NaN"`...)
	case math.IsInf(f, 1):
		return append(dst, `"+Inf"`...)
	case math.IsInf(f, -1):
		return append(dst, `"-Inf"`...)
	}
	abs := math.Abs(f)
	fmtc := byte('f')
	if abs != 0 {
		if bitSize == 64 && (abs < 1e-6 || abs >= 1e21) || bitSize == 32 && (float32(abs) < 1e-6 || float32(abs) >= 1e21) {
			fmtc = 'e'
		}
	}
	dst = strconv.AppendFloat(dst, f, fmtc, -1, bitSize)
	if fmtc == 'e' {
		// clean up e-09 to e-9, as encoding/json does
		n := len(dst)
		if n >= 4 && dst[n-4] == 'e' && dst[n-3] == '-' && dst[n-2] == '0' {
			dst[n-2] = dst[n-1]
			dst = dst[:n-1]
		}
	}
	return dst
}

func appendString(dst []byte, s string) []byte {
	b, _ := json.Marshal(s) // never fails for strings; escapes like encoding/json
	return append(dst, b...)
}
