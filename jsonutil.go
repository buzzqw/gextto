package gextto

import "reflect"

// normalizeJSON makes JSON responses faithful to the wire format the UI
// expects: empty collections serialize as `[]` / `{}` instead of `null`
// (slices and maps never serialize as null). Pointers keep their `null`
// semantics for `Option`.
//
// It rewrites slices, arrays, maps, interfaces and pointers recursively but
// leaves structs, scalars and custom marshalers untouched, so it is cheap and
// cannot reorder or drop struct fields.
func normalizeJSON(value any) any {
	rv := reflect.ValueOf(value)
	if !rv.IsValid() {
		return nil
	}
	return normalizeReflect(rv)
}

func normalizeReflect(rv reflect.Value) any {
	switch rv.Kind() {
	case reflect.Interface, reflect.Pointer:
		if rv.IsNil() {
			return nil
		}
		return normalizeReflect(rv.Elem())
	case reflect.Slice:
		if rv.IsNil() {
			return []any{}
		}
		out := make([]any, rv.Len())
		for index := 0; index < rv.Len(); index++ {
			out[index] = normalizeReflect(rv.Index(index))
		}
		return out
	case reflect.Array:
		out := make([]any, rv.Len())
		for index := 0; index < rv.Len(); index++ {
			out[index] = normalizeReflect(rv.Index(index))
		}
		return out
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			return rv.Interface()
		}
		if rv.IsNil() {
			return map[string]any{}
		}
		out := make(map[string]any, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			out[iter.Key().String()] = normalizeReflect(iter.Value())
		}
		return out
	default:
		return rv.Interface()
	}
}
