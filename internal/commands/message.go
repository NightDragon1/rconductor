// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package commands

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/NightDragon1/rconductor/internal/commands/sayconf"
	"github.com/NightDragon1/rconductor/internal/foreign"
	"github.com/NightDragon1/rconductor/internal/profiles"
	"github.com/NightDragon1/rconductor/internal/rcon/source"
	"github.com/NightDragon1/rconductor/internal/store/sqlitedb"
	"github.com/NightDragon1/rconductor/internal/users"
)

// maxSender bounds the sender (D-081).
const maxSender = 32

// senderName is the pseudonym as the players see it (D-081): without §,
// control, format (among them bidi and zero-width) characters, at most
// maxSender characters.
func senderName(pseudonym string) string {
	return cleanPart(pseudonym, maxSender)
}

// cleanPart drops control and format characters and §, trims and bounds a
// part of a message to maxRunes characters.
func cleanPart(s string, maxRunes int) string {
	s = foreign.Name(strings.ReplaceAll(s, "§", ""), utf8.RuneCountInString(s)+1)
	if utf8.RuneCountInString(s) > maxRunes {
		s = strings.TrimSpace(string([]rune(s)[:maxRunes]))
	}
	return s
}

// tellrawPart is one part of a minecraft-tellraw message: text only, no
// color, no events (D-083).
type tellrawPart struct {
	Text string `json:"text"`
}

// tellrawRoot is the root object every Minecraft Java version from 1.7.10
// on accepts (D-083).
type tellrawRoot struct {
	Text  string        `json:"text"`
	Extra []tellrawPart `json:"extra"`
}

// renderMessage builds the command sent for a message in the fixed form of
// render (D-081, D-083). Prefix, sender and text are JSON strings; quotes,
// backslashes and control characters are escaped, § as §. The second
// result is false for an unknown render.
func renderMessage(render, prefix, sender, text string) (string, bool) {
	if render != profiles.RenderMinecraftTellraw {
		return "", false
	}
	root := tellrawRoot{Text: ""}
	if prefix != "" {
		root.Extra = append(root.Extra, tellrawPart{Text: prefix + " "})
	}
	root.Extra = append(root.Extra, tellrawPart{Text: sender + ": "}, tellrawPart{Text: text})
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(root); err != nil {
		return "", false
	}
	out := strings.TrimSuffix(b.String(), "\n")
	return "tellraw @a " + strings.ReplaceAll(out, "\u00a7", `\u00a7`), true
}

// messageText is the text of a message command: what follows its path, or
// false if cmd is not the message command or has no text.
func messageText(cmd string, m *profiles.MessageCommand, caseSensitive bool) (string, bool) {
	if m == nil {
		return "", false
	}
	// The path as the profile check normalizes it, compared like a list
	// entry: whole first token, case by the profile (D-081).
	path := profiles.NormalizePath(m.Path)
	head, rest, _ := strings.Cut(cmd, " ")
	same := head == path
	if !caseSensitive {
		same = strings.EqualFold(head, path)
	}
	rest = strings.TrimSpace(rest)
	if !same || rest == "" {
		return "", false
	}
	return rest, true
}

// sayPrefix is the prefix for a server: that of the first of its groups,
// by name in byte order and then by ID, that has one (D-082).
func (s *Service) sayPrefix(ctx context.Context, serverID string) (string, error) {
	// Only the groups of this server, in one read: a say costs no more
	// than its own groups (CMD-020).
	ids, err := s.st.Queries.ListGroupsOfServer(ctx, serverID)
	if err != nil {
		return "", err
	}
	for _, id := range ids {
		v, err := s.st.Queries.GetSetting(ctx, sqlitedb.GetSettingParams{Key: sayconf.Setting, ScopeType: sayconf.ScopeServerGroup, ScopeID: id})
		if errors.Is(err, sql.ErrNoRows) {
			continue // no prefix for this group
		}
		if err != nil {
			// A read that fails is no "no prefix": the say is refused.
			return "", err
		}
		var p string
		if json.Unmarshal([]byte(v), &p) == nil {
			if p = cleanPart(p, sayconf.MaxPrefix); p != "" {
				return p, nil
			}
		}
	}
	return "", nil
}

// message is the command to send for cmd: the message in the form of the
// profile when cmd is the message command with a text, otherwise cmd
// itself (CMD-020, D-081).
func (s *Service) message(ctx context.Context, req users.Request, serverID, cmd string, syn Syntax, bound bool) (string, error) {
	text, ok := messageText(cmd, syn.Message, syn.CaseSensitive)
	if !ok {
		return cmd, nil
	}
	prefix, err := s.sayPrefix(ctx, serverID)
	if err != nil {
		return "", err
	}
	built, ok := renderMessage(syn.Message.Render, prefix, senderName(req.Pseudonym), text)
	if !ok {
		return "", s.deny(ctx, req, serverID, cmd, ReasonInvalid, 0)
	}
	// For those bound to lists, the built command holds no separator
	// either, whatever the escaping made of the text (CMD-005).
	if bound && HasSeparator(built, syn) {
		return "", s.deny(ctx, req, serverID, cmd, ReasonInvalid, 0)
	}
	if tooLong(built, syn) {
		return "", s.deny(ctx, req, serverID, cmd, ReasonMessageTooLong, 0)
	}
	return built, nil
}

// tooLong reports a command over what the server takes: the length of the
// profile in characters, and the bound of the protocol in bytes.
func tooLong(cmd string, syn Syntax) bool {
	return (syn.MaxLength > 0 && utf8.RuneCountInString(cmd) > syn.MaxLength) || len(cmd) > source.MaxCommand
}
