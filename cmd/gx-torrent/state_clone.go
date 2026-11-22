package main

import (
	"reflect"
	"time"
)

// cloneState makes a deep copy of the persisted state so json.MarshalIndent can
// run outside d.mu: at 1000 torrents the encoding is a few milliseconds, and
// holding the daemon lock for that long stalls every handler and tick. The copy
// is taken under the lock, so it is a consistent view of that moment.
func cloneState(in *persistedState) *persistedState {
	return deepCopyValue(reflect.ValueOf(in)).Interface().(*persistedState)
}

var timeType = reflect.TypeOf(time.Time{})

// deepCopyValue copies every value that can be reached from the state through
// a pointer, a map, a slice or an interface. Structs are first copied by value
// (scalars and the unexported fields of time.Time need no deeper treatment),
// then their exported fields are copied again as their own values.
func deepCopyValue(v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Interface:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		out := reflect.New(v.Type()).Elem()
		out.Set(deepCopyValue(v.Elem()))
		return out
	case reflect.Pointer:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		out := reflect.New(v.Type().Elem())
		out.Elem().Set(deepCopyValue(v.Elem()))
		return out
	case reflect.Map:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		out := reflect.MakeMapWithSize(v.Type(), v.Len())
		iter := v.MapRange()
		for iter.Next() {
			out.SetMapIndex(deepCopyValue(iter.Key()), deepCopyValue(iter.Value()))
		}
		return out
	case reflect.Slice:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			out.Index(i).Set(deepCopyValue(v.Index(i)))
		}
		return out
	case reflect.Array:
		out := reflect.New(v.Type()).Elem()
		for i := 0; i < v.Len(); i++ {
			out.Index(i).Set(deepCopyValue(v.Index(i)))
		}
		return out
	case reflect.Struct:
		out := reflect.New(v.Type()).Elem()
		out.Set(v)
		if v.Type() == timeType {
			return out
		}
		for i := 0; i < v.NumField(); i++ {
			if !v.Type().Field(i).IsExported() {
				continue
			}
			out.Field(i).Set(deepCopyValue(v.Field(i)))
		}
		return out
	default:
		// Scalars, channels and funcs are copied by value: the state only
		// carries scalars of these kinds.
		return v
	}
}
