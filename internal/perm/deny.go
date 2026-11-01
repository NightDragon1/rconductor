// SPDX-License-Identifier: AGPL-3.0-or-later

package perm

import (
	"log/slog"

	"github.com/NightDragon1/rconductor/internal/logging"
)

// Denial describes a refused access for the log (Design 8.3, perm.denied).
type Denial struct {
	RequestID  string
	SessionRef string
	User       string // pseudonym
	Role       Role
	Server     string // server ID, if the access was about a server
	Reason     string // reason code as in the API (Design 3.5)
	Action     string
}

// LogDenied writes perm.denied.
func LogDenied(log *slog.Logger, d Denial) {
	attrs := []any{
		logging.Event(logging.PermDenied), slog.String(logging.KeyReason, d.Reason),
		slog.String("action", d.Action),
	}
	for k, v := range map[string]string{
		logging.KeyRequestID: d.RequestID, logging.KeySession: d.SessionRef,
		logging.KeyUser: d.User, logging.KeyRole: string(d.Role), logging.KeyServer: d.Server,
	} {
		if v != "" {
			attrs = append(attrs, slog.String(k, v))
		}
	}
	log.Info("permission denied", attrs...)
}
