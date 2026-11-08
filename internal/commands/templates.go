// SPDX-License-Identifier: AGPL-3.0-or-later

package commands

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/NightDragon1/rconductor/internal/audit"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/sqlitedb"
	"github.com/NightDragon1/rconductor/internal/users"
)

// Fixed limits of a command template (CMD-008); the number per server is
// the setting commands.templates_per_server.
const (
	MaxTemplateName         = 64
	MaxTemplatePlaceholders = 16
)

// placeholder is {name} with a lower-case name; other braces are text, so
// that JSON in a command stays possible.
var placeholder = regexp.MustCompile(`\{([a-z][a-z0-9_]{0,31})\}`)

// Template is a command with placeholders of one server (CMD-008, D-068).
type Template struct {
	ID           string
	ServerID     string
	Name         string
	Template     string
	Placeholders []string
	Version      int64
}

// Placeholders returns the names of the placeholders of a template in
// order, each once.
func Placeholders(template string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, m := range placeholder.FindAllStringSubmatch(template, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}

// FixedPart returns the tokens of a template before the first token with a
// placeholder: the part that is the same whatever is filled in.
func FixedPart(template string) []string {
	var out []string
	for _, t := range Tokens(template) {
		if placeholder.MatchString(t) {
			break
		}
		out = append(out, t)
	}
	return out
}

// TemplateVisible reports whether a moderator or supporter with this list
// sees a template: its fixed part must start with an allowed path, by
// whole tokens like a command (CMD-004), and must not be a wrapper. The
// pipeline checks the filled command again (D-068).
func TemplateVisible(template string, list []string, syn Syntax) bool {
	fixed := FixedPart(template)
	if len(fixed) == 0 || IsWrapper(fixed, syn) {
		return false
	}
	return AllowedByList(fixed, list, syn.CaseSensitive)
}

// checkTemplate normalizes name and text of a template with the syntax of
// the server's profile.
func checkTemplate(name, text string, syn Syntax) (string, string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > MaxTemplateName || strings.ContainsFunc(name, isControl) {
		return "", "", &users.InvalidError{Detail: fmt.Sprintf("a name has 1 to %d characters", MaxTemplateName)}
	}
	cmd, err := Clean(text, syn)
	if err != nil {
		return "", "", &users.InvalidError{Detail: fmt.Sprintf("a template is a command of 1 to %d characters", syn.MaxLength)}
	}
	if t := Tokens(cmd); len(t) == 0 || placeholder.MatchString(t[0]) {
		return "", "", &users.InvalidError{Detail: "the first word of a template is fixed text"}
	}
	if len(placeholder.FindAllString(cmd, -1)) > MaxTemplatePlaceholders {
		return "", "", &users.InvalidError{Detail: fmt.Sprintf("a template has at most %d placeholders", MaxTemplatePlaceholders)}
	}
	return name, cmd, nil
}

func isControl(r rune) bool { return r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) }

func toTemplate(id, serverID, name, text string, version int64) Template {
	return Template{ID: id, ServerID: serverID, Name: name, Template: text, Placeholders: Placeholders(text), Version: version}
}

// scope returns the rights of the caller on the server; outside his scope
// the server does not exist for him.
func (s *Service) scope(ctx context.Context, req users.Request, serverID, action string) (*perm.Rights, perm.ServerRights, error) {
	rights, err := s.perm.For(ctx, req.Actor.ID)
	if err != nil {
		return nil, perm.ServerRights{}, err
	}
	sr, ok := rights.Server(serverID)
	if !ok {
		s.templateDenied(ctx, req, serverID, "", action, "not_found")
		return nil, perm.ServerRights{}, users.ErrNotFound
	}
	return rights, sr, nil
}

// manage allows changes of templates to the RCON admin and the server
// admin in scope (CMD-008). The role comes before any lookup, so that a
// moderator learns nothing about servers and templates from the answer
// (revision 2.4 candidate 14).
func (s *Service) manage(ctx context.Context, req users.Request, serverID, templateID, action string) error {
	if req.Actor.Role != perm.RCONAdmin && req.Actor.Role != perm.ServerAdmin {
		s.templateDenied(ctx, req, "", templateID, action, "forbidden")
		return users.ErrForbidden
	}
	_, _, err := s.scope(ctx, req, serverID, action)
	return err
}

func (s *Service) templateDenied(ctx context.Context, req users.Request, serverID, templateID, action, reason string) {
	perm.LogDenied(s.log, perm.Denial{
		RequestID: req.RequestID, SessionRef: req.SessionRef, User: req.Pseudonym,
		Role: req.Actor.Role, Server: serverID, Reason: reason, Action: action,
	})
	if reason == "not_found" {
		serverID = "" // a server outside the scope is not named
	}
	if _, err := audit.Write(ctx, s.st.DB, audit.Entry{
		Kind: audit.KindAdmin, ActorID: req.Actor.ID, ActorRole: string(req.Actor.Role), SessionID: req.SessionID,
		ServerID: serverID, TargetType: "template", TargetID: templateID, Action: action, Result: audit.ResultDenied, Reason: reason,
	}); err != nil {
		s.log.Error("audit entry not written", "action", action, "err", err)
	}
}

func (s *Service) templateAudit(ctx context.Context, tx *sql.Tx, req users.Request, serverID, templateID, action string, payload any) error {
	_, err := audit.Write(ctx, tx, audit.Entry{
		Kind: audit.KindAdmin, ActorID: req.Actor.ID, ActorRole: string(req.Actor.Role), SessionID: req.SessionID,
		ServerID: serverID, TargetType: "template", TargetID: templateID, Action: action, Payload: payload, Result: audit.ResultOK,
	})
	return err
}

// Templates lists the templates of a server the caller sees (D-068).
func (s *Service) Templates(ctx context.Context, req users.Request, serverID string) ([]Template, error) {
	_, sr, err := s.scope(ctx, req, serverID, "template.list")
	if err != nil {
		return nil, err
	}
	syn, err := s.syntax(ctx, serverID)
	if err != nil {
		return nil, err
	}
	rows, err := s.st.Queries.ListTemplates(ctx, serverID)
	if err != nil {
		return nil, err
	}
	out := []Template{}
	for _, r := range rows {
		if !sr.AllCommands && !TemplateVisible(r.Template, sr.Commands, syn) {
			continue
		}
		out = append(out, toTemplate(r.ID, r.ServerID, r.Name, r.Template, r.Version))
	}
	return out, nil
}

// GetTemplate returns one template, if the caller sees it.
func (s *Service) GetTemplate(ctx context.Context, req users.Request, serverID, id string) (Template, error) {
	ts, err := s.Templates(ctx, req, serverID)
	if err != nil {
		return Template{}, err
	}
	for _, t := range ts {
		if t.ID == id {
			return t, nil
		}
	}
	return Template{}, users.ErrNotFound
}

func (s *Service) templatesPerServer(ctx context.Context) int64 {
	n := users.DefaultTemplatesPerServer
	if err := s.st.GlobalSetting(ctx, users.SettingTemplatesPerServer, &n); err != nil || n < users.MinTemplatesPerServer {
		n = users.DefaultTemplatesPerServer
	}
	return int64(n)
}

func nameTaken(err error) error {
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return &users.InvalidError{Detail: "this server has a template with this name already"}
	}
	return err
}

// CreateTemplate adds a template to a server.
func (s *Service) CreateTemplate(ctx context.Context, req users.Request, serverID, name, text string) (Template, error) {
	if err := s.manage(ctx, req, serverID, "", "template.create"); err != nil {
		return Template{}, err
	}
	syn, err := s.syntax(ctx, serverID)
	if err != nil {
		return Template{}, err
	}
	name, text, err = checkTemplate(name, text, syn)
	if err != nil {
		return Template{}, err
	}
	id, err := store.NewID()
	if err != nil {
		return Template{}, err
	}
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return Template{}, err
	}
	defer func() { _ = tx.Rollback() }()
	now := store.Now()
	limit := s.templatesPerServer(ctx)
	n, err := sqlitedb.New(tx).InsertTemplate(ctx, sqlitedb.InsertTemplateParams{
		ID: id, ServerID: serverID, Name: name, Template: text, CreatedBy: req.Actor.ID,
		CreatedAt: now, UpdatedAt: now, MaxTemplates: limit,
	})
	if err != nil {
		return Template{}, nameTaken(err)
	}
	if n == 0 {
		return Template{}, &users.InvalidError{Detail: fmt.Sprintf("a server has at most %d templates", limit)}
	}
	if err := s.templateAudit(ctx, tx, req, serverID, id, "template.create", map[string]string{"name": name, "template": text}); err != nil {
		return Template{}, err
	}
	if err := tx.Commit(); err != nil {
		return Template{}, err
	}
	return toTemplate(id, serverID, name, text, 1), nil
}

// UpdateTemplate changes name or text of a template; the version read
// before is required (D-051).
func (s *Service) UpdateTemplate(ctx context.Context, req users.Request, serverID, id string, name, text *string, version *int64) (Template, error) {
	if err := s.manage(ctx, req, serverID, id, "template.update"); err != nil {
		return Template{}, err
	}
	cur, err := s.st.Queries.GetTemplate(ctx, sqlitedb.GetTemplateParams{ID: id, ServerID: serverID})
	if errors.Is(err, sql.ErrNoRows) {
		return Template{}, users.ErrNotFound
	}
	if err != nil {
		return Template{}, err
	}
	if version == nil {
		return Template{}, users.ErrPreconditionRequired
	}
	newName, newText := cur.Name, cur.Template
	if name != nil {
		newName = *name
	}
	if text != nil {
		newText = *text
	}
	syn, err := s.syntax(ctx, serverID)
	if err != nil {
		return Template{}, err
	}
	if newName, newText, err = checkTemplate(newName, newText, syn); err != nil {
		return Template{}, err
	}
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return Template{}, err
	}
	defer func() { _ = tx.Rollback() }()
	n, err := sqlitedb.New(tx).UpdateTemplate(ctx, sqlitedb.UpdateTemplateParams{
		Name: newName, Template: newText, UpdatedAt: store.Now(), ID: id, ServerID: serverID, Version: *version,
	})
	if err != nil {
		return Template{}, nameTaken(err)
	}
	if n == 0 {
		return Template{}, users.ErrConflict
	}
	if err := s.templateAudit(ctx, tx, req, serverID, id, "template.update", map[string]string{"name": newName, "template": newText}); err != nil {
		return Template{}, err
	}
	if err := tx.Commit(); err != nil {
		return Template{}, err
	}
	return toTemplate(id, serverID, newName, newText, *version+1), nil
}

// DeleteTemplate deletes a template softly.
func (s *Service) DeleteTemplate(ctx context.Context, req users.Request, serverID, id string) error {
	if err := s.manage(ctx, req, serverID, id, "template.delete"); err != nil {
		return err
	}
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	now := store.Now()
	n, err := sqlitedb.New(tx).SoftDeleteTemplate(ctx, sqlitedb.SoftDeleteTemplateParams{
		DeletedAt: sql.NullString{String: now, Valid: true}, UpdatedAt: now, ID: id, ServerID: serverID,
	})
	if err != nil {
		return err
	}
	if n == 0 {
		return users.ErrNotFound
	}
	if err := s.templateAudit(ctx, tx, req, serverID, id, "template.delete", nil); err != nil {
		return err
	}
	return tx.Commit()
}
