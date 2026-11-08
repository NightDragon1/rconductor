// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/NightDragon1/rconductor/internal/doccheck"
	"github.com/NightDragon1/rconductor/internal/store"
)

// TestSettingDefaultsAreDocumented covers DOC-001: the default of every
// setting in the configuration guide is the value a new panel stores on
// its first start. That a row exists for every setting and that its limits
// hold is checked in internal/users.
func TestSettingDefaultsAreDocumented(t *testing.T) {
	doc, err := os.ReadFile("../../docs/user/configuration.md")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := doccheck.Section(string(doc), "Settings in the panel")
	if err != nil {
		t.Fatal(err)
	}
	cfg := testConfig(t, t.TempDir())
	var out, console syncBuffer
	_, stop := startPanel(t, cfg, &out, &console)
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	st, err := store.Open(ctx, cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	stored, err := st.Queries.ListGlobalSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]json.RawMessage{}
	for _, s := range stored {
		values[s.Key] = json.RawMessage(s.Value)
	}
	for key, row := range rows {
		raw, ok := values[key]
		if !ok {
			t.Errorf("%s: a new panel stores no default", key)
			continue
		}
		if want := doccheck.Plain(row["Default"]); !sameValue(row["Type"], want, raw) {
			t.Errorf("%s: guide says default %q, panel stores %s", key, want, raw)
		}
	}
}

func sameValue(kind, guide string, raw json.RawMessage) bool {
	switch kind {
	case "duration":
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return false
		}
		g, err1 := doccheck.Duration(guide)
		c, err2 := time.ParseDuration(s)
		return err1 == nil && err2 == nil && g == c
	case "number":
		var n int
		return json.Unmarshal(raw, &n) == nil && strconv.Itoa(n) == guide
	case "boolean":
		var b bool
		return json.Unmarshal(raw, &b) == nil && map[bool]string{true: "on", false: "off"}[b] == guide
	}
	return false
}
