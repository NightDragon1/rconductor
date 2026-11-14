// SPDX-License-Identifier: AGPL-3.0-or-later

// Package features holds the feature flags (NFR-019): the global setting
// features.<name>, off by default, switches a new function.
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
type Flag struct {
	Name       string
	Operations []string
}

// production lists the flags of this build. A flag without operations
// switches only the interface; the backend checks every request anyway.
var production = []Flag{
	{Name: "server_save_as_new"}, // Save as New in the server dialog (UI-017)
	{Name: "log_filter_split"},   // two-part log filter, frontend only (UI-019)
	{Name: "command_palette"},    // command palette in the console (CMD-016)
	{Name: "profile_browser", Operations: []string{"GetServerProfile"}}, // profile browser (UI-020)
}

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

// Set is a checked list of flags; a nil Set has none.
type Set struct {
	names []string
	byOp  map[string]string
}

// New checks the flags; tests build their own set.
func New(flags ...Flag) (*Set, error) {
	s := &Set{byOp: map[string]string{}}
	for _, f := range flags {
		if !namePattern.MatchString(f.Name) || slices.Contains(s.names, f.Name) {
			return nil, fmt.Errorf("feature flag %q: invalid or duplicate name", f.Name)
		}
		s.names = append(s.names, f.Name)
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

// EnsureDefaults stores every flag as off unless it is already set.
func (s *Set) EnsureDefaults(ctx context.Context, st *store.Store) error {
	for _, name := range s.list() {
		if _, err := st.EnsureDefault(ctx, Key(name), false); err != nil {
			return err
		}
	}
	return nil
}

// Enabled reads a flag; a missing or unreadable value counts as off.
func (s *Set) Enabled(ctx context.Context, st *store.Store, name string) bool {
	var on bool
	return s.Known(Key(name)) && st != nil && st.GlobalSetting(ctx, Key(name), &on) == nil && on
}

// EnabledNames returns the names of the flags that are on.
func (s *Set) EnabledNames(ctx context.Context, st *store.Store) []string {
	return slices.DeleteFunc(append([]string{}, s.list()...), func(n string) bool { return !s.Enabled(ctx, st, n) })
}

func (s *Set) list() []string {
	if s == nil {
		return nil
	}
	return s.names
}
