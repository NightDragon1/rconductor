// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package servers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/NightDragon1/rconductor/internal/commands/sayconf"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/sqlitedb"
	"github.com/NightDragon1/rconductor/internal/users"
)

// The setting, its scope and its bound belong to the command pipeline,
// which builds the message from it (CMD-020, D-082).
const (
	SettingSayPrefix = sayconf.Setting
	ScopeServerGroup = sayconf.ScopeServerGroup
	MaxSayPrefix     = sayconf.MaxPrefix
)

// Audit and refusal actions of the prefix; their own, so a refused write is
// not logged as a read of the group.
const (
	actionSayPrefixRead   = "server_group.say_prefix.read"
	actionSayPrefixUpdate = "server_group.say_prefix.update"
)

// sayPrefixAccess lets the RCON admin and a server admin with every server
// of the group in scope through (D-082): other roles get ErrForbidden, a
// group that is unknown or not wholly in scope ErrNotFound, as outside the
// scope (Design 3.1, D-051).
func (s *Service) sayPrefixAccess(ctx context.Context, req users.Request, id, action string) error {
	if err := s.mayReadGroups(ctx, req, action, id); err != nil {
		return err
	}
	if _, err := s.st.Queries.GetServerGroup(ctx, id); errors.Is(err, sql.ErrNoRows) {
		s.denied(ctx, req, action, "server_group", id, "not_found")
		return users.ErrNotFound
	} else if err != nil {
		return err
	}
	ok, err := s.groupInScope(ctx, req, id)
	if err != nil {
		return err
	}
	if !ok {
		s.denied(ctx, req, action, "server_group", id, "not_found")
		return users.ErrNotFound
	}
	return nil
}

// SayPrefix returns the prefix of a group, "" without one (D-082).
func (s *Service) SayPrefix(ctx context.Context, req users.Request, id string) (string, error) {
	if err := s.sayPrefixAccess(ctx, req, id, actionSayPrefixRead); err != nil {
		return "", err
	}
	return readSayPrefix(ctx, s.st.Queries, id)
}

func readSayPrefix(ctx context.Context, q *sqlitedb.Queries, id string) (string, error) {
	v, err := q.GetSetting(ctx, sqlitedb.GetSettingParams{Key: SettingSayPrefix, ScopeType: ScopeServerGroup, ScopeID: id})
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var p string
	if err := json.Unmarshal([]byte(v), &p); err != nil {
		return "", err
	}
	return p, nil
}

// checkSayPrefix: at most MaxSayPrefix printable characters without §;
// surrounding space is trimmed, "" removes the prefix (D-082).
func checkSayPrefix(p string) (string, error) {
	p = strings.TrimSpace(p)
	bad := !utf8.ValidString(p) || utf8.RuneCountInString(p) > MaxSayPrefix || strings.ContainsRune(p, '§') ||
		strings.ContainsFunc(p, func(r rune) bool { return !unicode.IsPrint(r) && r != ' ' })
	if bad {
		return "", &users.InvalidError{Detail: "say_prefix: at most 16 printable characters without §"}
	}
	return p, nil
}

// SetSayPrefix stores the prefix of a group (D-082). The version is that
// of the group: it is checked and raised in the same transaction as the
// prefix is written, so a concurrent change of the group gets 409. Rights
// come before the version (D-051); the change is in the audit.
func (s *Service) SetSayPrefix(ctx context.Context, req users.Request, id, prefix string, present bool, version *int64) (string, error) {
	if err := s.sayPrefixAccess(ctx, req, id, actionSayPrefixUpdate); err != nil {
		return "", err
	}
	// The field is required: "" removes the prefix, a missing one or null
	// changes nothing and is refused (CMD-020).
	if !present {
		return "", &users.InvalidError{Detail: "say_prefix is required; \"\" removes the prefix"}
	}
	p, err := checkSayPrefix(prefix)
	if err != nil {
		return "", err
	}
	if version == nil {
		return "", users.ErrPreconditionRequired
	}
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	// The transaction holds the write lock from its start: the scope read
	// again now cannot change before the commit.
	if ok, err := s.groupInScope(ctx, req, id, true); err != nil {
		return "", err
	} else if !ok {
		s.denied(ctx, req, actionSayPrefixUpdate, "server_group", id, "not_found")
		return "", users.ErrNotFound
	}
	q := sqlitedb.New(tx)
	n, err := q.BumpServerGroupVersion(ctx, sqlitedb.BumpServerGroupVersionParams{ID: id, Version: *version})
	if err != nil {
		return "", err
	}
	if n == 0 {
		return "", users.ErrConflict
	}
	old, err := readSayPrefix(ctx, q, id)
	if err != nil {
		return "", err
	}
	if p == "" {
		err = q.DeleteScopedSetting(ctx, sqlitedb.DeleteScopedSettingParams{Key: SettingSayPrefix, ScopeType: ScopeServerGroup, ScopeID: id})
	} else {
		b, _ := json.Marshal(p)
		var sid string
		if sid, err = store.NewID(); err == nil {
			err = q.UpsertScopedSetting(ctx, sqlitedb.UpsertScopedSettingParams{
				ID: sid, Key: SettingSayPrefix, Value: string(b), ScopeType: ScopeServerGroup, ScopeID: id, UpdatedAt: store.Now(),
			})
		}
	}
	if err != nil {
		return "", err
	}
	if err := s.auditOK(ctx, tx, req, actionSayPrefixUpdate, "server_group", id, "", map[string]string{"old": old, "new": p}); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return p, nil
}
