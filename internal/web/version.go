// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"context"
	"strconv"
	"strings"

	"github.com/NightDragon1/rconductor/internal/store/sqlitedb"
)

// Optimistic locking for objects saved as a whole (revision 2.4
// candidate 24): reading gives the stored version, a write names it in
// If-Match, and the database checks and raises it in one statement, so
// that it holds with Postgres and several panels too (NFR-011). Without
// If-Match the write is refused (428) after the role check; a stale or
// unreadable version gives 409.

// ifMatch reads the version named in If-Match; nil when there is none. A
// value that is no version can never match.
func ifMatch(h *string) *int64 {
	if h == nil {
		return nil
	}
	v, err := strconv.ParseInt(strings.Trim(strings.TrimPrefix(strings.TrimSpace(*h), "W/"), `"`), 10, 64)
	if err != nil {
		v = -1
	}
	return &v
}

func versionString(v int64) string { return strconv.FormatInt(v, 10) }

// versions maps the IDs of one kind of object to their versions.
type versions map[string]string

func (s *apiServer) groupVersions(ctx context.Context) versions {
	rows, _ := s.store.Queries.ListUserGroupVersions(ctx)
	return mapVersions(rows, func(r sqlitedb.ListUserGroupVersionsRow) (string, int64) { return r.ID, r.Version })
}

func (s *apiServer) listVersions(ctx context.Context) versions {
	rows, _ := s.store.Queries.ListCommandListVersions(ctx)
	return mapVersions(rows, func(r sqlitedb.ListCommandListVersionsRow) (string, int64) { return r.ID, r.Version })
}

func (s *apiServer) serverVersions(ctx context.Context) versions {
	rows, _ := s.store.Queries.ListServerVersions(ctx)
	return mapVersions(rows, func(r sqlitedb.ListServerVersionsRow) (string, int64) { return r.ID, r.Version })
}

func (s *apiServer) serverGroupVersions(ctx context.Context) versions {
	rows, _ := s.store.Queries.ListServerGroupVersions(ctx)
	return mapVersions(rows, func(r sqlitedb.ListServerGroupVersionsRow) (string, int64) { return r.ID, r.Version })
}

func mapVersions[R any](rows []R, f func(R) (string, int64)) versions {
	out := versions{}
	for _, r := range rows {
		id, v := f(r)
		out[id] = versionString(v)
	}
	return out
}

// settingsETag is the version of the global settings as ETag.
func (s *apiServer) settingsETag(ctx context.Context) *string {
	v, err := s.users.SettingsVersion(ctx)
	if err != nil {
		return nil
	}
	e := `"` + versionString(v) + `"`
	return &e
}
