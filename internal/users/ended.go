// SPDX-License-Identifier: AGPL-3.0-or-later

package users

import (
	"context"
	"database/sql"

	"github.com/NightDragon1/rconductor/internal/audit"
)

// ActionAssignmentRemoved is the audit action of an assignment that ended
// with the server, server group or user group it named (ROLE-003, ROLE-004,
// ROLE-007). Migration 00018 writes the same action for the assignments
// left behind by earlier deletions.
const ActionAssignmentRemoved = "assignment.remove"

// Removed is one assignment that ended with the object it named. Section
// is the name of the section in the assignments of the user (Design 2.2):
// admin_scopes, grants or supporter_grants.
type Removed struct {
	Section string
	ID      string
	// TargetType and Target name who held it: a user, or a user group for
	// a grant to the whole group.
	TargetType string
	Target     string
	ServerID   string
	// Entry names what the assignment pointed at, as in the section.
	Entry map[string]string
}

// WriteRemoved writes one audit entry per removed assignment inside tx, so
// that the deletion and its trail commit together. cause is the action
// that ended them.
func WriteRemoved(ctx context.Context, tx *sql.Tx, req Request, cause string, list []Removed) error {
	for _, r := range list {
		if _, err := audit.Write(ctx, tx, audit.Entry{
			Kind: audit.KindAdmin, ActorID: req.Actor.ID, ActorRole: string(req.Actor.Role), SessionID: req.SessionID,
			ServerID: r.ServerID, TargetType: r.TargetType, TargetID: r.Target, Action: ActionAssignmentRemoved,
			Payload: map[string]any{"section": r.Section, "id": r.ID, "entry": r.Entry, "cause": cause},
			Result:  audit.ResultOK,
		}); err != nil {
			return err
		}
	}
	return nil
}

// GrantHolder names who holds a grant: the member named, or the group.
func GrantHolder(group string, user sql.NullString) (targetType, target string) {
	if user.Valid {
		return "user", user.String
	}
	return "user_group", group
}
