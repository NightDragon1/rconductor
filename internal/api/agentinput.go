// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import "encoding/json"

// missingDisplayName marks an AgentInput without the field display_name:
// null removes the name, a missing field is refused, after the role check
// (UI-021, D-051).
var missingDisplayName = new(string)

// DisplayNameMissing reports a body without the field display_name.
func (a AgentInput) DisplayNameMissing() bool { return a.DisplayName == missingDisplayName }

// UnmarshalJSON tells a missing display_name from null, which the
// generated type cannot.
func (a *AgentInput) UnmarshalJSON(b []byte) error {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	raw, ok := m["display_name"]
	switch {
	case !ok:
		a.DisplayName = missingDisplayName
		return nil
	case string(raw) == "null":
		a.DisplayName = nil
		return nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return err
	}
	a.DisplayName = &s
	return nil
}

// missingSayPrefix stands for a SayPrefixInput without the field or with
// null: a control character, so the check refuses it after the rights
// (CMD-020, D-051); "" removes the prefix.
const missingSayPrefix = "\x00"

// SayPrefixMissing reports a body without say_prefix or with null.
func (p SayPrefixInput) SayPrefixMissing() bool { return p.SayPrefix == missingSayPrefix }

// UnmarshalJSON tells a missing or null say_prefix from "".
func (p *SayPrefixInput) UnmarshalJSON(b []byte) error {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	raw, ok := m["say_prefix"]
	if !ok || string(raw) == "null" {
		p.SayPrefix = missingSayPrefix
		return nil
	}
	return json.Unmarshal(raw, &p.SayPrefix)
}
