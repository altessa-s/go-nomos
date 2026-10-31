// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package yaml3

import (
	"bytes"
	"errors"
	"io"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/altessa-s/go-nomos/loader/backend"
)

// yaml.v3's limits on alias expansion (decode.go: allowedAliasRatio and the
// check in unmarshal), reproduced on the node tree.
const (
	aliasRatioRangeLow  = 400000
	aliasRatioRangeHigh = 4000000
	minCheckedAliases   = 100
	// maxAliasRatio and minAliasRatio bound the allowed share of
	// alias-expanded decodes, for small and very large documents.
	maxAliasRatio     = 0.99
	minAliasRatio     = 0.10
	minCheckedDecodes = 1000
	// expansionCap saturates node counts so a hostile document cannot
	// overflow them. Counts are int64 on every platform; two capped counts
	// add up to at most 2^41, so min(a+b, expansionCap) never overflows.
	expansionCap int64 = 1 << 40
	// maxKeyComparisons bounds the key pairs yaml.v3 compares while it
	// decodes a document: it checks every pair of keys of a mapping, each
	// time an alias makes it decode the mapping again.
	maxKeyComparisons int64 = 100_000_000
	// nodesPerEntry is the key and value nodes of a mapping entry.
	nodesPerEntry = 2
)

var (
	// errAliasCycle reports an anchor whose value contains an alias to it.
	errAliasCycle = errors.New("yaml: anchor value contains itself")
	// errExcessiveAliasing reports more alias expansion than yaml.v3 allows.
	errExcessiveAliasing = errors.New("yaml: document contains excessive aliasing")
	// errDuplicateMappingKey reports a mapping with a repeated key. It does not name
	// the key, which may be sensitive.
	errDuplicateMappingKey = errors.New("yaml: mapping contains a duplicate key")
	// errExcessiveMappingWork reports mappings whose key comparisons exceed
	// maxKeyComparisons.
	errExcessiveMappingWork = errors.New("yaml: document mappings are too large")
)

// readChecked reads the document of reader, rejects it when checkMappings
// does, and returns a reader over its content for yaml.v3 to decode.
func readChecked(reader io.Reader) (io.Reader, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.NewDecoder(bytes.NewReader(data)).Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return bytes.NewReader(data), nil
		}
		return nil, err
	}
	if err := checkMappings(&doc); err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

// checkMappings rejects, in linear time, a mapping with two keys of the same
// kind and value, which yaml.v3 finds by comparing every pair of keys, and a
// document whose mappings would make yaml.v3 compare more than
// maxKeyComparisons key pairs.
func checkMappings(doc *yaml.Node) error {
	g := &mappingGuard{costs: make(map[*yaml.Node]int64), inProgress: make(map[*yaml.Node]bool)}
	cost, err := g.cost(doc)
	if err != nil {
		return err
	}
	if cost > maxKeyComparisons {
		return errExcessiveMappingWork
	}
	return nil
}

// mappingKey identifies a mapping key as yaml.v3's duplicate check does.
type mappingKey struct {
	kind  yaml.Kind
	value string
}

// mappingGuard measures the key comparisons of a node tree, aliases followed.
type mappingGuard struct {
	costs      map[*yaml.Node]int64
	inProgress map[*yaml.Node]bool
}

// cost returns the key pairs yaml.v3 compares to decode n, capped at
// expansionCap, and fails on a mapping with a duplicate key. An alias cycle
// adds nothing: decoding reports it.
func (g *mappingGuard) cost(n *yaml.Node) (int64, error) {
	if n == nil {
		return 0, nil
	}
	if c, ok := g.costs[n]; ok {
		return c, nil
	}
	if g.inProgress[n] {
		return 0, nil
	}
	g.inProgress[n] = true
	defer delete(g.inProgress, n)

	var total int64
	if n.Kind == yaml.MappingNode {
		pairs := int64(len(n.Content) / nodesPerEntry)
		total = min(pairs*(pairs-1)/nodesPerEntry, expansionCap)
		seen := make(map[mappingKey]struct{}, pairs)
		for i := 0; i+1 < len(n.Content); i += nodesPerEntry {
			key := mappingKey{kind: n.Content[i].Kind, value: n.Content[i].Value}
			if _, dup := seen[key]; dup {
				return 0, errDuplicateMappingKey
			}
			seen[key] = struct{}{}
		}
	}
	children := n.Content
	if n.Kind == yaml.AliasNode {
		children = []*yaml.Node{n.Alias}
	}
	for _, child := range children {
		c, err := g.cost(child)
		if err != nil {
			return 0, err
		}
		total = min(total+c, expansionCap)
	}
	g.costs[n] = total
	return total, nil
}

// CheckDocument parses the first YAML document of content into nodes and
// rejects what yaml.v3 would refuse to decode for structural reasons: an
// anchor that contains an alias to itself, and excessive aliasing, measured
// with yaml.v3's own ratio of alias-expanded nodes to all decoded nodes. It
// decodes nothing into Go values, so any document yaml.v3 decodes into the
// configuration (typed map keys included) passes.
func (b *Backend) CheckDocument(content string) error {
	var doc yaml.Node
	if err := yaml.NewDecoder(strings.NewReader(content)).Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	if err := checkMappings(&doc); err != nil {
		return err
	}

	c := &aliasCounter{sizes: make(map[*yaml.Node]int64), inProgress: make(map[*yaml.Node]bool)}
	if err := c.walk(&doc); err != nil {
		return err
	}
	if c.aliased > minCheckedAliases && c.decodes > minCheckedDecodes &&
		float64(c.aliased)/float64(c.decodes) > allowedAliasRatio(c.decodes) {
		return errExcessiveAliasing
	}
	return nil
}

// aliasCounter measures alias expansion over a node tree. decodes counts
// every node yaml.v3 would decode; aliased the ones it decodes inside an
// alias expansion.
type aliasCounter struct {
	sizes      map[*yaml.Node]int64
	inProgress map[*yaml.Node]bool
	decodes    int64
	aliased    int64
}

// size returns the number of nodes n expands to, aliases followed, capped
// at expansionCap; an anchor whose value contains itself fails.
func (c *aliasCounter) size(n *yaml.Node) (int64, error) {
	if n == nil {
		return 0, nil
	}
	if s, ok := c.sizes[n]; ok {
		return s, nil
	}
	if c.inProgress[n] {
		return 0, errAliasCycle
	}
	c.inProgress[n] = true
	defer delete(c.inProgress, n)

	total := int64(1)
	children := n.Content
	if n.Kind == yaml.AliasNode {
		children = []*yaml.Node{n.Alias}
	}
	for _, child := range children {
		s, err := c.size(child)
		if err != nil {
			return 0, err
		}
		total = min(total+s, expansionCap)
	}
	c.sizes[n] = total
	return total, nil
}

// walk counts the decodes of n and its descendants.
func (c *aliasCounter) walk(n *yaml.Node) error {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.AliasNode {
		s, err := c.size(n.Alias)
		if err != nil {
			return err
		}
		c.decodes = min(c.decodes+1+s, expansionCap)
		c.aliased = min(c.aliased+s, expansionCap)
		return nil
	}
	c.decodes = min(c.decodes+1, expansionCap)
	for _, child := range n.Content {
		if err := c.walk(child); err != nil {
			return err
		}
	}
	return nil
}

// allowedAliasRatio is yaml.v3's allowed share of alias-expanded decodes.
func allowedAliasRatio(decodes int64) float64 {
	switch {
	case decodes <= aliasRatioRangeLow:
		return maxAliasRatio
	case decodes >= aliasRatioRangeHigh:
		return minAliasRatio
	default:
		return maxAliasRatio - (maxAliasRatio-minAliasRatio)*
			(float64(decodes-aliasRatioRangeLow)/float64(aliasRatioRangeHigh-aliasRatioRangeLow))
	}
}

// Ensure Backend implements backend.DocumentChecker.
var _ backend.DocumentChecker = (*Backend)(nil)
