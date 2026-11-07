// SPDX-License-Identifier: AGPL-3.0-or-later

package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// StateFile keeps the pairing in the secrets directory next to the agent
// key (Design 1.2).
const StateFile = "panel.json"

// State is what the agent knows about its pairing.
type State struct {
	// AgentID is assigned by the panel at pairing.
	AgentID string `json:"agent_id,omitempty"`
	// PanelAddr and PanelFingerprint are the pinned panel.
	PanelAddr        string `json:"panel_addr,omitempty"`
	PanelFingerprint string `json:"panel_fingerprint,omitempty"`
	// TokenHash marks the token that made the pairing, so that the same
	// token in the environment is ignored afterwards.
	TokenHash string `json:"token_hash,omitempty"`
	// Revoked stops automatic reconnects until a new pairing (Design 1.7).
	Revoked bool `json:"revoked,omitempty"`
}

func loadState(dir string) (State, error) {
	var s State
	b, err := os.ReadFile(filepath.Join(dir, StateFile)) //nolint:gosec // path from configuration
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, fmt.Errorf("read pairing state: %w", err)
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return s, fmt.Errorf("pairing state %s: %w", StateFile, err)
	}
	return s, nil
}

// save writes the state atomically with mode 0600.
func (s State) save(dir string) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".panel-*.json")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, StateFile))
}
