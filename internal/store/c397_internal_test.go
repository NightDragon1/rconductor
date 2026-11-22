// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package store

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
)

// TestC397EndedContextDecodeNoLog (D-109): a decode failure seen by a
// request that ended is no fault to log either.
func TestC397EndedContextDecodeNoLog(t *testing.T) {
	var out bytes.Buffer
	s := &Store{}
	ended, cancel := context.WithCancel(context.Background())
	cancel()
	s.logReadFailed(ended, slog.New(slog.NewJSONHandler(&out, nil)), "login.max_failures", &json.SyntaxError{}, "decode")
	if out.Len() != 0 {
		t.Errorf("logged for an ended request: %s", out.String())
	}
	s.logReadFailed(context.Background(), slog.New(slog.NewJSONHandler(&out, nil)), "login.max_failures", &json.SyntaxError{}, "decode")
	if out.Len() == 0 {
		t.Error("a decode failure of a live request is not logged")
	}
}
