// SPDX-License-Identifier: AGPL-3.0-or-later

package profiles

import (
	"fmt"

	"github.com/goccy/go-yaml"
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
	anchors := anchorsOf(file.Docs[0].Body)
	for _, top := range entries(file.Docs[0].Body, anchors, 0) {
		key, _ := keyName(top.Key, anchors)
		out[key] = true
		for _, sub := range entries(top.Value, anchors, 0) {
			name, _ := keyName(sub.Key, anchors)
			out[key+"."+name] = true
		}
	}
	return out
}

// anchorsOf maps each anchor name to its value; a name defined twice
// means its last value, as for the decoder, which reads all anchors of
// the document before it decodes the fields.
func anchorsOf(body ast.Node) map[string]ast.Node {
	anchors := map[string]ast.Node{}
	for _, n := range ast.Filter(ast.AnchorType, body) {
		if a, ok := n.(*ast.AnchorNode); ok && a.Name != nil {
			anchors[a.Name.String()] = a.Value
		}
	}
	return anchors
}

// checkKeys refuses a mapping key that does not decode to a string: the
// decoder drops a mapping with such a key without an error, while its
// other keys would count as set (D-074).
func checkKeys(file *ast.File) []string {
	var probs []string
	for _, doc := range file.Docs {
		if doc == nil || doc.Body == nil {
			continue
		}
		anchors := anchorsOf(doc.Body)
		for _, n := range ast.Filter(ast.MappingValueType, doc.Body) {
			mv, ok := n.(*ast.MappingValueNode)
			if !ok || mv.Key == nil {
				continue
			}
			if _, ok := keyName(mv.Key, anchors); !ok {
				probs = append(probs, fmt.Sprintf("%s a key must be a string", at(mv.Key.GetToken())))
			}
		}
	}
	return probs
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
		if name, _ := keyName(mv.Key, anchors); name == "<<" {
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

// keyName is the name a mapping key decodes to, as the decoder converts
// it: quotes, escapes, tags and block scalars ("? >-", "? |-") resolved,
// also for an explicit "? key", an anchored key and an alias. ok is false
// for a key that does not decode to a string.
func keyName(k ast.Node, anchors map[string]ast.Node) (name string, ok bool) {
	for range maxDepth {
		switch v := k.(type) {
		case nil:
			return "", false
		case *ast.MergeKeyNode:
			return "<<", true
		case *ast.StringNode:
			return v.Value, true
		case *ast.LiteralNode:
			if v.Value == nil {
				return "", false
			}
			return v.Value.Value, true
		case *ast.MappingKeyNode:
			k = v.Value
			continue
		case *ast.AnchorNode:
			k = v.Value
			continue
		case *ast.AliasNode:
			if v.Value == nil {
				return "", false
			}
			k = anchors[v.Value.String()]
			continue
		}
		// Tagged and other scalars: the decoder's own conversion.
		var val any
		if err := yaml.NodeToValue(k, &val); err != nil {
			return "", false
		}
		s, isString := val.(string)
		return s, isString
	}
	return "", false
}
