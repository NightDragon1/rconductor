// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"go/constant"
	"go/token"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/ssa"
)

// logOrder tells whether a call of RequestData orders container log lines:
// its order type is logs.tail or logs.follow, or no constant, which may be
// either (UI-022).
func logOrder(site ssa.CallInstruction) bool {
	if site == nil {
		return true
	}
	cc := site.Common()
	i := 2 // ctx, agentID, typ
	if !cc.IsInvoke() {
		i = 3 // the receiver comes first
	}
	if i >= len(cc.Args) {
		return true
	}
	c, ok := cc.Args[i].(*ssa.Const)
	if !ok || c.Value == nil || c.Value.Kind() != constant.String {
		return true
	}
	typ := constant.StringVal(c.Value)
	return typ == "logs.tail" || typ == "logs.follow"
}

// inFiles tells whether fn lies in one of files (paths below root; one
// ending in "/" stands for a directory).
func inFiles(fset *token.FileSet, fn *ssa.Function, root string, files []string) bool {
	if fn == nil || !fn.Pos().IsValid() {
		return false
	}
	rel, err := filepath.Rel(root, fset.Position(fn.Pos()).Filename)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	for _, f := range files {
		if rel == f || strings.HasSuffix(f, "/") && strings.HasPrefix(rel, f) {
			return true
		}
	}
	return false
}
