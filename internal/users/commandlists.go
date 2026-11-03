// SPDX-License-Identifier: AGPL-3.0-or-later

package users

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/sqlitedb"
)

// maxPathLen bounds a token path of a list entry.
const maxPathLen = 256

// Entry is a token path of a command list (CMD-004); passable entries may
// be passed on to supporters (ROLE-007).
type Entry struct {
	Path     string
	Passable bool
}

// CommandList is a list of allowed commands for a moderator group.
type CommandList struct {
	ID      string
	Name    string
	OwnerID string
	Entries []Entry
}

// normalizePath trims a token path and joins its tokens with single
// spaces. Control characters are refused; the command pipeline removes
// them anyway (CMD-006).
func normalizePath(p string) (string, error) {
	for _, r := range p {
		if unicode.IsControl(r) && r != '\t' {
			return "", &InvalidError{Detail: "a command path must not contain control characters"}
		}
	}
	p = strings.Join(strings.Fields(p), " ")
	if p == "" || utf8.RuneCountInString(p) > maxPathLen {
		return "", &InvalidError{Detail: fmt.Sprintf("a command path has 1 to %d characters", maxPathLen)}
	}
	return p, nil
}

// checkEntries normalizes entries and enforces that at least one command
// of a non-empty list stays locked for supporters (ROLE-007, Design 3.6).
func checkEntries(in []Entry) ([]Entry, error) {
	out := make([]Entry, 0, len(in))
	seen := map[string]bool{}
	locked := false
	for _, e := range in {
		p, err := normalizePath(e.Path)
		if err != nil {
			return nil, err
		}
		if seen[p] {
			return nil, &InvalidError{Detail: fmt.Sprintf("the command %q is listed twice", p)}
		}
		seen[p] = true
		locked = locked || !e.Passable
		out = append(out, Entry{Path: p, Passable: e.Passable})
	}
	if len(out) > 0 && !locked {
		return nil, &InvalidError{Detail: "at least one command of the list must stay locked for supporters"}
	}
	return out, nil
}

// CommandLists returns the lists the caller administers: all for the RCON
// admin, his own for a server admin.
func (s *Service) CommandLists(ctx context.Context, req Request) ([]CommandList, error) {
	if !mayAdministerGroups(req.Actor.Role) {
		s.deniedOn(ctx, req, "command_list.list", "command_list", "", "forbidden", nil)
		return nil, ErrForbidden
	}
	rows, err := s.st.Queries.ListCommandLists(ctx)
	if err != nil {
		return nil, fmt.Errorf("list command lists: %w", err)
	}
	out := []CommandList{}
	for _, r := range rows {
		if req.Actor.Role != perm.RCONAdmin && r.OwnerID != req.Actor.ID {
			continue
		}
		l, err := s.listDetails(ctx, sqlitedb.GetCommandListRow(r))
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, nil
}

func (s *Service) listDetails(ctx context.Context, r sqlitedb.GetCommandListRow) (CommandList, error) {
	rows, err := s.st.Queries.ListCommandListEntries(ctx, r.ID)
	if err != nil {
		return CommandList{}, err
	}
	l := CommandList{ID: r.ID, Name: r.Name, OwnerID: r.OwnerID, Entries: []Entry{}}
	for _, e := range rows {
		l.Entries = append(l.Entries, Entry{Path: e.Path, Passable: e.Passable == 1})
	}
	return l, nil
}

func (s *Service) commandList(ctx context.Context, req Request, id, action string) (sqlitedb.GetCommandListRow, error) {
	if !mayAdministerGroups(req.Actor.Role) {
		s.deniedOn(ctx, req, action, "command_list", id, "forbidden", nil)
		return sqlitedb.GetCommandListRow{}, ErrForbidden
	}
	l, err := s.st.Queries.GetCommandList(ctx, id)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && req.Actor.Role != perm.RCONAdmin && l.OwnerID != req.Actor.ID) {
		s.deniedOn(ctx, req, action, "command_list", id, "not_found", nil)
		return l, ErrNotFound
	}
	return l, err
}

// CommandList returns a list the caller administers.
func (s *Service) CommandList(ctx context.Context, req Request, id string) (CommandList, error) {
	l, err := s.commandList(ctx, req, id, "command_list.read")
	if err != nil {
		return CommandList{}, err
	}
	return s.listDetails(ctx, l)
}

func writeEntries(ctx context.Context, q *sqlitedb.Queries, listID string, entries []Entry) error {
	if err := q.DeleteCommandListEntries(ctx, listID); err != nil {
		return err
	}
	for _, e := range entries {
		id, err := store.NewID()
		if err != nil {
			return err
		}
		passable := int64(0)
		if e.Passable {
			passable = 1
		}
		if err := q.InsertCommandListEntry(ctx, sqlitedb.InsertCommandListEntryParams{ID: id, CommandListID: listID, Path: e.Path, Passable: passable}); err != nil {
			return err
		}
	}
	return nil
}

// CreateCommandList creates a list owned by the caller. Any command may be
// listed, also one that is not in a catalog (CAT-002).
func (s *Service) CreateCommandList(ctx context.Context, req Request, name string, entries []Entry) (CommandList, error) {
	if !mayAdministerGroups(req.Actor.Role) {
		s.deniedOn(ctx, req, "command_list.create", "command_list", "", "forbidden", nil)
		return CommandList{}, ErrForbidden
	}
	name, err := validName(name)
	if err != nil {
		return CommandList{}, err
	}
	if entries, err = checkEntries(entries); err != nil {
		return CommandList{}, err
	}
	id, err := store.NewID()
	if err != nil {
		return CommandList{}, err
	}
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return CommandList{}, err
	}
	defer func() { _ = tx.Rollback() }()
	q := sqlitedb.New(tx)
	now := store.Now()
	if err := q.InsertCommandList(ctx, sqlitedb.InsertCommandListParams{ID: id, Name: name, OwnerID: req.Actor.ID, CreatedAt: now, UpdatedAt: now}); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return CommandList{}, &InvalidError{Detail: "you already have a list with this name"}
		}
		return CommandList{}, err
	}
	if err := writeEntries(ctx, q, id, entries); err != nil {
		return CommandList{}, err
	}
	if err := s.auditOn(ctx, tx, req, "command_list.create", "command_list", id, map[string]any{"name": name, "entries": entries}); err != nil {
		return CommandList{}, err
	}
	if err := tx.Commit(); err != nil {
		return CommandList{}, err
	}
	return s.CommandList(ctx, req, id)
}

// UpdateCommandList renames a list or replaces its entries. The rights of
// moderators and supporters follow on their next request; their sessions
// stay, because their own assignments did not change.
func (s *Service) UpdateCommandList(ctx context.Context, req Request, id string, name *string, entries *[]Entry) (CommandList, error) {
	l, err := s.commandList(ctx, req, id, "command_list.update")
	if err != nil {
		return CommandList{}, err
	}
	newName := l.Name
	if name != nil {
		if newName, err = validName(*name); err != nil {
			return CommandList{}, err
		}
	}
	var checked []Entry
	if entries != nil {
		if checked, err = checkEntries(*entries); err != nil {
			return CommandList{}, err
		}
	}
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return CommandList{}, err
	}
	defer func() { _ = tx.Rollback() }()
	q := sqlitedb.New(tx)
	changes := map[string]any{}
	if newName != l.Name {
		if err := q.RenameCommandList(ctx, sqlitedb.RenameCommandListParams{Name: newName, UpdatedAt: store.Now(), ID: id}); err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				return CommandList{}, &InvalidError{Detail: "the owner already has a list with this name"}
			}
			return CommandList{}, err
		}
		changes["name"] = newName
	}
	if entries != nil {
		if err := writeEntries(ctx, q, id, checked); err != nil {
			return CommandList{}, err
		}
		changes["entries"] = checked
	}
	if len(changes) > 0 {
		if err := s.auditOn(ctx, tx, req, "command_list.update", "command_list", id, changes); err != nil {
			return CommandList{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return CommandList{}, err
	}
	s.perm.Invalidate()
	return s.CommandList(ctx, req, id)
}

// DeleteCommandList soft-deletes a list; groups that use it grant no
// commands any more.
func (s *Service) DeleteCommandList(ctx context.Context, req Request, id string) error {
	l, err := s.commandList(ctx, req, id, "command_list.delete")
	if err != nil {
		return err
	}
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	now := store.Now()
	if err := sqlitedb.New(tx).SoftDeleteCommandList(ctx, sqlitedb.SoftDeleteCommandListParams{DeletedAt: sql.NullString{String: now, Valid: true}, UpdatedAt: now, ID: id}); err != nil {
		return err
	}
	if err := s.auditOn(ctx, tx, req, "command_list.delete", "command_list", id, map[string]string{"name": l.Name}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.perm.Invalidate()
	return nil
}
