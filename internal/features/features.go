// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package features holds the feature flags (NFR-019): the global setting
// features.<name> switches a function; each flag has its default.
package features

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/NightDragon1/rconductor/internal/store"
)

// KeyPrefix starts the settings key of every flag.
const KeyPrefix = "features."

// Flag guards operation IDs as the strict handler names them (ListServers).
// On is the default; a stored value wins over it.
type Flag struct {
	Name       string
	Operations []string
	On         bool
}

// production lists the flags of this build. A flag without operations
// switches only the interface; the backend checks every request anyway.
// The flags of 1.2 left with 1.3; stored values of them stay unused.
var production = []Flag{}

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

// Set is a checked list of flags; a nil Set has none.
type Set struct {
	names []string
	byOp  map[string]string
	on    map[string]bool
}

// New checks the flags; tests build their own set.
func New(flags ...Flag) (*Set, error) {
	s := &Set{byOp: map[string]string{}, on: map[string]bool{}}
	for _, f := range flags {
		if !namePattern.MatchString(f.Name) || slices.Contains(s.names, f.Name) {
			return nil, fmt.Errorf("feature flag %q: invalid or duplicate name", f.Name)
		}
		s.names = append(s.names, f.Name)
		s.on[f.Name] = f.On
		for _, op := range f.Operations {
			if _, ok := s.byOp[op]; ok {
				return nil, fmt.Errorf("operation %s: guarded twice", op)
			}
			s.byOp[op] = f.Name
		}
	}
	slices.Sort(s.names)
	return s, nil
}

// Default returns the flags of this build.
func Default() *Set {
	s, err := New(production...)
	if err != nil {
		panic(err)
	}
	return s
}

// Key is the settings key of a flag.
func Key(name string) string { return KeyPrefix + name }

// Known reports whether key is the settings key of a flag of the set.
func (s *Set) Known(key string) bool {
	name, ok := strings.CutPrefix(key, KeyPrefix)
	return ok && s != nil && slices.Contains(s.names, name)
}

// ForOperation returns the flag that guards an operation.
func (s *Set) ForOperation(op string) (string, bool) {
	if s == nil {
		return "", false
	}
	name, ok := s.byOp[op]
	return name, ok
}

// EnsureDefaults stores every flag with its default unless it is already set.
func (s *Set) EnsureDefaults(ctx context.Context, st *store.Store) error {
	for _, name := range s.list() {
		if _, err := st.EnsureDefault(ctx, Key(name), s.on[name]); err != nil {
			return err
		}
	}
	return nil
}

// Enabled reads a flag; a missing or unreadable value counts as its default.
func (s *Set) Enabled(ctx context.Context, st *store.Store, name string) bool {
	if !s.Known(Key(name)) {
		return false
	}
	on := s.on[name]
	if st == nil || st.GlobalSetting(ctx, Key(name), &on) != nil {
		return s.on[name]
	}
	return on
}

func (s *Set) list() []string {
	if s == nil {
		return nil
	}
	return s.names
}
