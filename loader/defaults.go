// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package loader

import (
	"context"
	"reflect"
	"slices"

	loadersecrets "github.com/altessa-s/go-nomos/loader/secrets"
)

// applyDefaults applies Default() methods and default tags to the whole
// configuration in one traversal, after the files and the environment were
// loaded. Values a file or the environment set explicitly keep their value,
// even when it is false, 0 or "".
//
// For every struct — the configuration, nested pointer structs and the struct
// elements of maps and slices — Default runs first, then default tags fill the
// zero fields nothing set explicitly. A nil pointer struct is allocated when
// it has a field to default, unless it is tagged default:"-" or a file set it
// to null.
func (cf *loadState) applyDefaults(ctx context.Context) error {
	if cf.options.skipDefaults {
		return nil
	}
	defer func() { cf.defaulting, cf.aliasPresence = nil, nil }()
	root := reflect.ValueOf(cf.conf).Elem()
	cf.aliasPresence = gatherAliasPresence(root, cf.present)
	return cf.defaultStruct(ctx, root, cf.present, true, nil)
}

// gatherAliasPresence returns, for each struct reached through more than one
// path of root (two pointers to one struct), the presence of all its paths
// merged, so a value set explicitly through one of them is explicit for all.
func gatherAliasPresence(root reflect.Value, p *presence) map[ptrKey]*presence {
	w := aliasWalk{reached: make(map[ptrKey][]*presence), walking: make(map[ptrKey]bool)}
	w.walkStruct(root, p)
	return mergeAliasPresence(w.reached)
}

// aliasWalk is the state of gatherAliasPresence while it walks the
// configuration: the presence of every path reaching each addressable struct.
type aliasWalk struct {
	reached map[ptrKey][]*presence
	walking map[ptrKey]bool
	// copies keeps the copies of map values alive, so no two share an address.
	copies []reflect.Value
}

func (w *aliasWalk) walkStruct(v reflect.Value, p *presence) {
	if v.CanAddr() {
		key := ptrKey{t: reflect.PointerTo(v.Type()), p: v.Addr().Pointer()}
		w.reached[key] = append(w.reached[key], p)
		if w.walking[key] {
			return
		}
		w.walking[key] = true
		defer delete(w.walking, key)
	}
	for i := range v.NumField() {
		if v.Type().Field(i).IsExported() {
			w.walkValue(v.Field(i), p.field(i))
		}
	}
}

func (w *aliasWalk) walkValue(v reflect.Value, p *presence) {
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() && v.Elem().Kind() == reflect.Struct {
			w.walkStruct(v.Elem(), p)
		}
	case reflect.Struct:
		w.walkStruct(v, p)
	case reflect.Map:
		if v.IsNil() || !hasStructElements(v.Type().Elem()) {
			return
		}
		for it := v.MapRange(); it.Next(); {
			value := reflect.New(v.Type().Elem()).Elem()
			value.Set(it.Value())
			w.copies = append(w.copies, value)
			w.walkValue(value, p.entry(it.Key()))
		}
	case reflect.Slice, reflect.Array:
		if !hasStructElements(v.Type().Elem()) {
			return
		}
		for i := range v.Len() {
			w.walkValue(v.Index(i), p.elem(i))
		}
	default:
	}
}

// mergeAliasPresence merges, for each struct reached through more than one
// path, the presence of all those paths (see unionPresence).
func mergeAliasPresence(reached map[ptrKey][]*presence) map[ptrKey]*presence {
	var merged map[ptrKey]*presence
	for key, all := range reached {
		if len(all) <= 1 {
			continue
		}
		var m *presence
		for _, ap := range all {
			m = unionPresence(m, ap)
		}
		if merged == nil {
			merged = make(map[ptrKey]*presence)
		}
		merged[key] = m
	}
	return merged
}

// unionPresence returns the presence of a value reached through two paths
// with presences a and b: whatever either path set explicitly is explicit.
// Unlike mergePresence, no path replaces what the other set.
func unionPresence(a, b *presence) *presence {
	switch {
	case a == nil:
		return b
	case b == nil:
		return a
	}
	out := &presence{set: a.set || b.set, envGrown: a.envGrown || b.envGrown}
	switch {
	case a.envGrown && b.envGrown:
		out.envBase = min(a.envBase, b.envBase)
	case a.envGrown:
		out.envBase = a.envBase
	case b.envGrown:
		out.envBase = b.envBase
	}
	if a.fields != nil || b.fields != nil {
		out.fields = make(map[int]*presence, max(len(a.fields), len(b.fields)))
		for i, c := range a.fields {
			out.fields[i] = c
		}
		for i, c := range b.fields {
			out.fields[i] = unionPresence(out.fields[i], c)
		}
	}
	if a.entries != nil || b.entries != nil {
		out.entries = make(map[any]*presence, max(len(a.entries), len(b.entries)))
		for k, c := range a.entries {
			out.entries[k] = c
		}
		for k, c := range b.entries {
			out.entries[k] = unionPresence(out.entries[k], c)
		}
	}
	if a.elems != nil || b.elems != nil {
		out.elems = make([]*presence, max(len(a.elems), len(b.elems)))
		for i := range out.elems {
			out.elems[i] = unionPresence(a.elem(i), b.elem(i))
		}
	}
	// A null holds only when nothing below the value was set through a path.
	out.null = (a.null || b.null) && out.fields == nil && out.entries == nil && out.elems == nil
	return out
}

// ptrKey identifies the value a pointer points to: by address and type, as a
// struct and its first field share an address.
type ptrKey struct {
	t reflect.Type
	p uintptr
}

// keyOf returns the ptrKey of the non-nil pointer v.
func keyOf(v reflect.Value) ptrKey {
	return ptrKey{t: v.Type(), p: v.Pointer()}
}

// defaultStruct applies defaults to struct v, whose presence is p. Default
// runs when withDefaulter is set: for the configuration, pointer structs and
// collection elements, not for struct values or embedded structs. path lists
// the struct types being defaulted, so a recursive pointer type is not
// allocated forever.
func (cf *loadState) defaultStruct(ctx context.Context, v reflect.Value, p *presence, withDefaulter bool, path []reflect.Type) error {
	// A struct being defaulted further up the walk, reached again through a
	// pointer cycle (n.Next = n, or back to the root), or a struct reached
	// through several paths and defaulted already, is skipped: Default runs
	// once per struct, with the presence of all its paths.
	if v.CanAddr() {
		key := ptrKey{t: reflect.PointerTo(v.Type()), p: v.Addr().Pointer()}
		if _, done := cf.defaulting[key]; done {
			return nil
		}
		if cf.defaulting == nil {
			cf.defaulting = make(map[ptrKey]struct{})
		}
		cf.defaulting[key] = struct{}{}
		if merged, ok := cf.aliasPresence[key]; ok {
			p = merged
		} else {
			// Only a struct of the configuration's graph stays marked: the
			// address of a map value's copy may be reused by a later copy.
			defer delete(cf.defaulting, key)
		}
	}
	if withDefaulter {
		cf.callDefaulter(v, p)
	}

	t := v.Type()
	path = append(slices.Clip(path), t)
	for i := range t.NumField() {
		sf := t.Field(i)
		fv := v.Field(i)
		if sf.PkgPath != "" || !fv.CanSet() {
			continue
		}
		if err := cf.defaultField(ctx, fv, sf, p.field(i), path); err != nil {
			return err
		}
	}
	return nil
}

// defaultField applies defaults to the struct field fv described by sf.
func (cf *loadState) defaultField(ctx context.Context, fv reflect.Value, sf reflect.StructField, p *presence, path []reflect.Type) error {
	tag := sf.Tag.Get(defaultValueTagName)

	switch {
	case isStructPointer(sf.Type):
		if fv.IsNil() {
			if !allocatesPointer(sf, tag, p, path) {
				return nil
			}
			fv.Set(reflect.New(sf.Type.Elem()))
		}
		return cf.defaultStruct(ctx, fv.Elem(), p, !sf.Anonymous, path)
	case sf.Type.Kind() == reflect.Struct:
		return cf.defaultStruct(ctx, fv, p, false, path)
	}

	if tag != "" && !p.isSet() {
		if err := cf.applyTag(ctx, fv, defaultTagValue(tag), p); err != nil {
			return err
		}
	}
	return cf.defaultElements(ctx, fv, p, path)
}

// allocatesPointer reports whether a nil pointer struct field gets allocated:
// an embedded one always is, like the field list does; another one when its
// struct has a field to default, unless it is tagged default:"-", a file set
// it to null or its type is already being defaulted.
func allocatesPointer(sf reflect.StructField, tag string, p *presence, path []reflect.Type) bool {
	elem := sf.Type.Elem()
	if slices.Contains(path, elem) {
		return false
	}
	if sf.Anonymous {
		return true
	}
	return tag != "-" && !p.isNull() && allocatedByFieldList(elem, nil)
}

// applyTag assigns a default tag value to a field nothing set explicitly.
// The tag applies to a zero value; a slice or map the environment grew entry
// by entry instead gets the tag's entries in the holes the environment left.
func (cf *loadState) applyTag(ctx context.Context, fv reflect.Value, value string, p *presence) error {
	if !needsDefault(fv) {
		return cf.fillEnvCollection(ctx, fv, value, p)
	}

	substituted, err := cf.substituteDefault(value)
	if err != nil {
		return err
	}
	return cf.setDefault(ctx, fv, substituted)
}

// defaultSetError seals the parse error of a default tag resolved from a
// secret (see sealParseError): the parser's message may quote the secret. tag is the value after ${VAR} substitution, the one whose
// placeholders were resolved.
func defaultSetError(err error, tag string) error {
	if err == nil || !loadersecrets.HasSecrets(tag) {
		return err
	}
	return sealParseError(err, "the default tag", loadersecrets.References(tag))
}

// needsDefault reports whether fv, or the value it points to, is zero.
func needsDefault(fv reflect.Value) bool {
	for fv.Kind() == reflect.Pointer && !fv.IsNil() {
		fv = fv.Elem()
	}
	return fv.IsZero()
}

// fillEnvCollection adds the entries of a default tag to the holes of a slice
// or map the environment grew: slice elements it added without setting them
// that are still zero, and keys missing from a map it created. Entries that
// existed before — from the value passed to Load, a file or Default() — are
// never overwritten. A slice the environment created from nothing takes the
// tag's length.
func (cf *loadState) fillEnvCollection(ctx context.Context, fv reflect.Value, value string, p *presence) error {
	if p == nil || p.set || !p.envGrown || fv.Kind() != reflect.Slice && fv.Kind() != reflect.Map {
		return nil
	}
	if fv.Kind() == reflect.Slice && !hasEnvHole(fv, p) {
		// Nothing to fill: don't substitute (and in strict mode fail on) a
		// default that is never applied.
		return nil
	}

	substituted, err := cf.substituteDefault(value)
	if err != nil {
		return err
	}
	defaults := reflect.New(fv.Type()).Elem()
	if err := cf.setDefault(ctx, defaults, substituted); err != nil {
		return err
	}

	if fv.Kind() == reflect.Map {
		for it := defaults.MapRange(); it.Next(); {
			if !fv.MapIndex(it.Key()).IsValid() && p.entry(it.Key()) == nil {
				fv.SetMapIndex(it.Key(), it.Value())
			}
		}
		return nil
	}

	if n := defaults.Len(); p.envBase == 0 && n > fv.Len() {
		grown := reflect.MakeSlice(fv.Type(), n, n)
		reflect.Copy(grown, fv)
		fv.Set(grown)
	}
	for i := p.envBase; i < min(defaults.Len(), fv.Len()); i++ {
		if p.elem(i) == nil && fv.Index(i).IsZero() {
			fv.Index(i).Set(defaults.Index(i))
		}
	}
	return nil
}

// hasEnvHole reports whether an environment-grown slice has an element the
// default tag could fill: it was created from nothing, or an element past its
// pre-environment length is still unset and zero.
func hasEnvHole(fv reflect.Value, p *presence) bool {
	if p.envBase == 0 {
		return true
	}
	for i := p.envBase; i < fv.Len(); i++ {
		if p.elem(i) == nil && fv.Index(i).IsZero() {
			return true
		}
	}
	return false
}

// substituteDefault expands ${VAR} references in a default tag value being
// applied.
func (cf *loadState) substituteDefault(value string) (string, error) {
	if cf.options.strict {
		return substituteEnvVariablesStrict(value)
	}
	return substituteEnvVariables(value), nil
}

// setDefault resolves the $__secret{namespace:key} placeholders of a
// substituted default tag value, then parses it into fv; a parse error of a
// resolved value names its placeholders only (see defaultSetError).
func (cf *loadState) setDefault(ctx context.Context, fv reflect.Value, substituted string) error {
	resolved, err := cf.resolveSecrets(ctx, substituted)
	if err != nil {
		return err
	}
	_, err = set(fv, resolved, true, true, cf.options.strict)
	return defaultSetError(err, substituted)
}

// defaultElements applies defaults to the struct elements of a map, slice or
// array, recursing into nested collections. Map values are not addressable,
// so each one is defaulted as a copy and stored back.
func (cf *loadState) defaultElements(ctx context.Context, v reflect.Value, p *presence, path []reflect.Type) error {
	switch v.Kind() {
	case reflect.Map:
		if v.IsNil() || !hasStructElements(v.Type().Elem()) {
			return nil
		}
		for it := v.MapRange(); it.Next(); {
			value := reflect.New(v.Type().Elem()).Elem()
			value.Set(it.Value())
			if err := cf.defaultElement(ctx, value, p.entry(it.Key()), path); err != nil {
				return err
			}
			v.SetMapIndex(it.Key(), value)
		}
	case reflect.Slice, reflect.Array:
		if !hasStructElements(v.Type().Elem()) {
			return nil
		}
		for i := range v.Len() {
			if err := cf.defaultElement(ctx, v.Index(i), p.elem(i), path); err != nil {
				return err
			}
		}
	default:
	}
	return nil
}

// defaultElement applies defaults to an addressable map or slice element.
func (cf *loadState) defaultElement(ctx context.Context, v reflect.Value, p *presence, path []reflect.Type) error {
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		if v.Elem().Kind() == reflect.Struct {
			return cf.defaultStruct(ctx, v.Elem(), p, true, path)
		}
		v = v.Elem()
	}
	if v.Kind() == reflect.Struct {
		return cf.defaultStruct(ctx, v, p, true, path)
	}
	return cf.defaultElements(ctx, v, p, path)
}

// hasStructElements reports whether values of type t are, or hold through
// pointers and nested collections, structs that may need defaults.
func hasStructElements(t reflect.Type) bool {
	seen := make(map[reflect.Type]bool)
	for !seen[t] {
		seen[t] = true
		switch t.Kind() {
		case reflect.Struct:
			return true
		case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
			t = t.Elem()
		default:
			return false
		}
	}
	// A recursive type (type R []R) holds no struct.
	return false
}

// allocatedByFieldList reports whether a nil pointer to struct type t is
// allocated when applying defaults: it is when t, or a struct t embeds, has
// an exported field other than a pointer to a struct, whose default handling
// needs the struct. A struct without such fields, like time.Time, stays nil.
func allocatedByFieldList(t reflect.Type, visiting map[reflect.Type]bool) bool {
	if visiting[t] {
		return false
	}
	if visiting == nil {
		visiting = map[reflect.Type]bool{}
	}
	visiting[t] = true

	for sf := range t.Fields() {
		if sf.PkgPath != "" {
			continue
		}
		if et := derefType(sf.Type); sf.Anonymous && et.Kind() == reflect.Struct {
			if allocatedByFieldList(et, visiting) {
				return true
			}
			continue
		}
		if !isStructPointer(sf.Type) {
			return true
		}
	}
	return false
}

// isStructPointer reports whether t is a pointer to a struct.
func isStructPointer(t reflect.Type) bool {
	return t.Kind() == reflect.Pointer && t.Elem().Kind() == reflect.Struct
}

// callDefaulter runs Default on struct v when it implements Defaulter, keeping
// the values the files and the environment set: Default may fill or derive
// any other value, but every explicit value is restored after it runs. p is
// the presence of v.
func (cf *loadState) callDefaulter(v reflect.Value, p *presence) {
	if !v.CanAddr() {
		return
	}
	d, ok := reflect.TypeAssert[Defaulter](v.Addr())
	if !ok {
		return
	}

	explicit := takeSnapshot(v, p)
	d.Default()
	explicit.restore(v)
}

// snapshot holds independent copies of the explicitly set values under a
// value, shaped like its presence, so they can be written back after Default
// without discarding what Default set around them.
type snapshot struct {
	// value is the copy of a value restored whole: a scalar, an explicit
	// null, an explicitly empty slice or map, or a value whose structure is
	// unknown. It is invalid for a container restored child by child.
	value   reflect.Value
	fields  map[int]*snapshot
	entries []entrySnapshot
	elems   []*snapshot
	// length is the explicit length of a slice a file wrote, or -1 when only
	// some of its elements are explicit.
	length int
}

// entrySnapshot is the snapshot of a map entry.
type entrySnapshot struct {
	key  reflect.Value
	snap *snapshot
}

// takeSnapshot copies the explicitly set values under v, whose presence is p.
// It returns nil when nothing under v is explicit.
func takeSnapshot(v reflect.Value, p *presence) *snapshot {
	if p == nil || !v.IsValid() {
		return nil
	}
	if isWholeValue(v.Type(), p) {
		if !p.set && !p.null {
			return nil
		}
		return &snapshot{value: deepCopy(v)}
	}

	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}

	s := &snapshot{length: -1}
	switch v.Kind() {
	case reflect.Struct:
		s.fields = snapshotFields(v, p)
	case reflect.Map:
		s.entries = snapshotEntries(v, p)
	case reflect.Slice, reflect.Array:
		if p.set && v.Kind() == reflect.Slice {
			s.length = len(p.elems)
		}
		s.elems = make([]*snapshot, min(len(p.elems), v.Len()))
		for i := range s.elems {
			s.elems[i] = takeSnapshot(v.Index(i), p.elems[i])
		}
	default:
	}
	return s
}

// snapshotFields snapshots the explicit exported fields of struct v.
func snapshotFields(v reflect.Value, p *presence) map[int]*snapshot {
	fields := make(map[int]*snapshot, len(p.fields))
	for i, c := range p.fields {
		if !v.Type().Field(i).IsExported() {
			continue
		}
		if cs := takeSnapshot(v.Field(i), c); cs != nil {
			fields[i] = cs
		}
	}
	return fields
}

// snapshotEntries snapshots the explicit entries of map v whose decoded keys
// convert to its key type.
func snapshotEntries(v reflect.Value, p *presence) []entrySnapshot {
	var entries []entrySnapshot
	keyType := v.Type().Key()
	for k, c := range p.entries {
		key := reflect.ValueOf(k)
		if !key.IsValid() || !key.Type().ConvertibleTo(keyType) {
			continue
		}
		key = key.Convert(keyType)
		if cs := takeSnapshot(v.MapIndex(key), c); cs != nil {
			entries = append(entries, entrySnapshot{key: key, snap: cs})
		}
	}
	return entries
}

// isWholeValue reports whether a value of type t with presence p is restored
// whole rather than child by child.
func isWholeValue(t reflect.Type, p *presence) bool {
	if p.null {
		return true
	}
	switch derefType(t).Kind() {
	case reflect.Struct:
		return p.fields == nil
	case reflect.Map:
		return p.entries == nil || p.set && len(p.entries) == 0
	case reflect.Slice:
		return p.elems == nil || p.set && len(p.elems) == 0
	case reflect.Array:
		return p.elems == nil
	default:
		return true
	}
}

// restore writes the snapshot back into the settable value v, allocating
// pointers, maps and slice elements Default removed.
func (s *snapshot) restore(v reflect.Value) {
	if s == nil {
		return
	}
	if s.value.IsValid() {
		v.Set(s.value)
		return
	}

	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.Struct:
		for i, c := range s.fields {
			c.restore(v.Field(i))
		}
	case reflect.Map:
		if v.IsNil() {
			v.Set(reflect.MakeMap(v.Type()))
		}
		for _, e := range s.entries {
			value := reflect.New(v.Type().Elem()).Elem()
			if current := v.MapIndex(e.key); current.IsValid() {
				value.Set(current)
			}
			e.snap.restore(value)
			v.SetMapIndex(e.key, value)
		}
	case reflect.Slice:
		n := s.length
		if n < 0 {
			n = max(v.Len(), len(s.elems))
		}
		if v.Len() != n {
			resized := reflect.MakeSlice(v.Type(), n, n)
			reflect.Copy(resized, v)
			v.Set(resized)
		}
		for i, c := range s.elems {
			c.restore(v.Index(i))
		}
	case reflect.Array:
		for i, c := range s.elems {
			c.restore(v.Index(i))
		}
	default:
	}
}

// deepCopy returns an independent copy of v: pointers, maps, slices and
// interfaces reachable through exported fields are copied, not shared. A
// pointer cycle is copied as a cycle.
func deepCopy(v reflect.Value) reflect.Value {
	return deepCopySeen(v, make(map[ptrKey]reflect.Value))
}

// deepCopySeen is deepCopy with the copies made so far of each pointer.
func deepCopySeen(v reflect.Value, seen map[ptrKey]reflect.Value) reflect.Value {
	out := reflect.New(v.Type()).Elem()
	copyInto(out, v, seen)
	return out
}

// copyInto deep-copies src into the settable dst of the same type. seen maps
// each pointer copied so far to its copy.
func copyInto(dst, src reflect.Value, seen map[ptrKey]reflect.Value) {
	switch src.Kind() {
	case reflect.Pointer:
		copyPointer(dst, src, seen)
	case reflect.Map:
		copyMap(dst, src, seen)
	case reflect.Slice:
		if !src.IsNil() {
			s := reflect.MakeSlice(src.Type(), src.Len(), src.Len())
			copyElements(s, src, seen)
			dst.Set(s)
		}
	case reflect.Array:
		copyElements(dst, src, seen)
	case reflect.Interface:
		if !src.IsNil() {
			dst.Set(deepCopySeen(src.Elem(), seen))
		}
	case reflect.Struct:
		dst.Set(src)
		for i := range src.NumField() {
			if dst.Field(i).CanSet() {
				copyInto(dst.Field(i), src.Field(i), seen)
			}
		}
	default:
		dst.Set(src)
	}
}

// copyPointer deep-copies a non-nil pointer once: a pointer seen before gets
// its existing copy, keeping shared and cyclic references intact.
func copyPointer(dst, src reflect.Value, seen map[ptrKey]reflect.Value) {
	if src.IsNil() {
		return
	}
	key := keyOf(src)
	if p, ok := seen[key]; ok {
		dst.Set(p)
		return
	}
	p := reflect.New(src.Type().Elem())
	seen[key] = p
	copyInto(p.Elem(), src.Elem(), seen)
	dst.Set(p)
}

// copyMap deep-copies the values of a non-nil map into a new map.
func copyMap(dst, src reflect.Value, seen map[ptrKey]reflect.Value) {
	if src.IsNil() {
		return
	}
	m := reflect.MakeMapWithSize(src.Type(), src.Len())
	for it := src.MapRange(); it.Next(); {
		m.SetMapIndex(it.Key(), deepCopySeen(it.Value(), seen))
	}
	dst.Set(m)
}

// copyElements deep-copies each element of the slice or array src into dst,
// which has at least as many elements.
func copyElements(dst, src reflect.Value, seen map[ptrKey]reflect.Value) {
	for i := range src.Len() {
		copyInto(dst.Index(i), src.Index(i), seen)
	}
}
