// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package yaml3

import "gopkg.in/yaml.v3"

// CommentPaths reports mapping value paths carrying head or line comments,
// on the key ("# c" above it, "key: # c") or on the value. It does not follow
// aliases or interpret commented-out template lines.
func (b *Backend) CommentPaths(data []byte) ([][]string, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	var paths [][]string
	var walk func(*yaml.Node, []string)
	walk = func(n *yaml.Node, prefix []string) {
		if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
			walk(n.Content[0], prefix)
		}
		if n.Kind != yaml.MappingNode {
			return
		}
		for i := 0; i+1 < len(n.Content); i += 2 {
			path := append(append([]string(nil), prefix...), n.Content[i].Value)
			key, value := n.Content[i], n.Content[i+1]
			if key.LineComment != "" || key.HeadComment != "" || value.LineComment != "" || value.HeadComment != "" {
				paths = append(paths, path)
			}
			if value.Kind == yaml.MappingNode {
				walk(value, path)
			}
		}
	}
	walk(&doc, nil)
	return paths, nil
}
