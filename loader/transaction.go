// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package loader

import (
	"reflect"
	"slices"
)

// captureConfig records exported state in place, preserving aliases on rollback.
// Unexported state and external effects of user hooks cannot be rolled back.
func captureConfig(conf any) func() {
	if conf == nil {
		return func() {}
	}
	c := &configCapture{seen: make(map[visitKey]bool)}
	c.capture(reflect.ValueOf(conf))
	return func() {
		for _, undo := range slices.Backward(c.undo) {
			undo()
		}
	}
}

type configCapture struct {
	undo []func()
	seen map[visitKey]bool
}

// visitKey identifies a captured pointer, map or slice: by type too, as a
// pointer to an array and a pointer to its first element share an address.
type visitKey struct {
	typ reflect.Type
	ptr uintptr
	len int
}

func (c *configCapture) capture(v reflect.Value) {
	if !v.IsValid() {
		return
	}
	if v.CanSet() {
		saved := reflect.New(v.Type()).Elem()
		saved.Set(v)
		c.undo = append(c.undo, func() { v.Set(saved) })
	}
	if c.visited(v) {
		return
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			c.capture(v.Elem())
		}
	case reflect.Struct:
		for _, field := range v.Fields() {
			if field.CanInterface() {
				c.capture(field)
			}
		}
	case reflect.Map:
		c.captureMap(v)
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			c.capture(v.Index(i))
		}
	default:
	}
}
func (c *configCapture) visited(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice:
		if v.IsNil() {
			return true
		}
		key := visitKey{typ: v.Type(), ptr: v.Pointer()}
		if v.Kind() == reflect.Slice {
			key.len = v.Len()
		}
		if c.seen[key] {
			return true
		}
		c.seen[key] = true
	default:
	}
	return false
}
func (c *configCapture) captureMap(v reflect.Value) {
	original := reflect.ValueOf(v.Interface())
	saved := reflect.MakeMapWithSize(v.Type(), v.Len())
	for it := v.MapRange(); it.Next(); {
		saved.SetMapIndex(it.Key(), it.Value())
		c.capture(it.Value())
	}
	c.undo = append(c.undo, func() {
		original.Clear()
		for it := saved.MapRange(); it.Next(); {
			original.SetMapIndex(it.Key(), it.Value())
		}
	})
}
