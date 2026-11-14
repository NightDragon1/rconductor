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
