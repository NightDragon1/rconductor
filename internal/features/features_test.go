// SPDX-License-Identifier: AGPL-3.0-or-later

package features

import "testing"

func TestNewRefusesBadFlags(t *testing.T) {
	for _, bad := range [][]Flag{{{Name: ""}}, {{Name: "Upper"}}, {{Name: "a.b"}}, {{Name: "a"}, {Name: "a"}},
		{{Name: "a", Operations: []string{"X"}}, {Name: "b", Operations: []string{"X"}}}} {
		if _, err := New(bad...); err == nil {
			t.Errorf("accepted %v", bad)
		}
	}
}

func TestDefaultKnowsServerSaveAsNew(t *testing.T) {
	s := Default()
	if !s.Known(Key("server_save_as_new")) {
		t.Fatal("flag server_save_as_new missing")
	}
	if _, ok := s.ForOperation("CreateServer"); ok {
		t.Fatal("server_save_as_new must not guard an operation")
	}
}

func TestDefaultHoldsTheBuildFlags(t *testing.T) {
	s := Default()
	// Frontend-only flags guard no operation (UI-019).
	if !s.Known(Key("log_filter_split")) {
		t.Fatal("log_filter_split missing")
	}
	for _, op := range []string{"ListServers", "GetServer"} {
		if name, ok := s.ForOperation(op); ok {
			t.Errorf("%s guarded by %s", op, name)
		}
	}
	if s.Enabled(t.Context(), nil, "log_filter_split") {
		t.Error("flag on without a store")
	}
}
