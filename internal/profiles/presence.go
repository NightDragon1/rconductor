// SPDX-License-Identifier: AGPL-3.0-or-later

package profiles

import (
	"github.com/goccy/go-yaml/ast"
)

// presentKeys lists the keys the file writes, as dotted paths of the top
// level and one level below (syntax.case_sensitive, ports.game, …). It
// looks at the YAML nodes, not at the decoded values, so an explicit
// false, 0, "" or [] counts as set (D-074). Anchors, aliases and merge
// keys (<<) are followed; checkAliases has bounded them before.
func presentKeys(file *ast.File) map[string]bool {
	out := map[string]bool{}
	if file == nil || len(file.Docs) == 0 || file.Docs[0] == nil || file.Docs[0].Body == nil {
		return out
	}
	anchors := map[string]ast.Node{}
	for _, n := range ast.Filter(ast.AnchorType, file.Docs[0].Body) {
		if a, ok := n.(*ast.AnchorNode); ok && a.Name != nil {
			anchors[a.Name.String()] = a.Value
		}
	}
	for _, top := range entries(file.Docs[0].Body, anchors, 0) {
		key := keyName(top.Key)
		out[key] = true
		for _, sub := range entries(top.Value, anchors, 0) {
			out[key+"."+keyName(sub.Key)] = true
		}
	}
	return out
}

// entries are the key-value pairs of a mapping node, through anchors,
// aliases, tags and merge keys; anything else has none.
func entries(n ast.Node, anchors map[string]ast.Node, depth int) []*ast.MappingValueNode {
	if n == nil || depth > maxDepth {
		return nil
	}
	switch v := n.(type) {
	case *ast.AnchorNode:
		return entries(v.Value, anchors, depth+1)
	case *ast.AliasNode:
		if v.Value == nil {
			return nil
		}
		return entries(anchors[v.Value.String()], anchors, depth+1)
	case *ast.TagNode:
		return entries(v.Value, anchors, depth+1)
	case *ast.MappingValueNode:
		return expand([]*ast.MappingValueNode{v}, anchors, depth)
	case *ast.MappingNode:
		return expand(v.Values, anchors, depth)
	}
	return nil
}

func expand(vals []*ast.MappingValueNode, anchors map[string]ast.Node, depth int) []*ast.MappingValueNode {
	var out []*ast.MappingValueNode
	for _, mv := range vals {
		if mv.Key != nil && keyName(mv.Key) == "<<" {
			out = append(out, entries(mv.Value, anchors, depth+1)...)
			if seq, ok := mv.Value.(*ast.SequenceNode); ok {
				for _, item := range seq.Values {
					out = append(out, entries(item, anchors, depth+1)...)
				}
			}
			continue
		}
		out = append(out, mv)
	}
	return out
}

// keyName is the name a mapping key decodes to: without quotes, tag or
// anchor, also for an explicit "? key".
func keyName(k ast.Node) string {
	switch v := k.(type) {
	case nil:
		return ""
	case *ast.StringNode:
		return v.Value
	case *ast.MappingKeyNode:
		return keyName(v.Value)
	case *ast.TagNode:
		return keyName(v.Value)
	case *ast.AnchorNode:
		return keyName(v.Value)
	case *ast.MergeKeyNode:
		return "<<"
	}
	return k.String()
}
