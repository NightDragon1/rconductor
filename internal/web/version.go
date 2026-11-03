// SPDX-License-Identifier: AGPL-3.0-or-later

package web

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/NightDragon1/rconductor/internal/api"
	"github.com/NightDragon1/rconductor/internal/perm"
)

// Optimistic locking for objects saved as a whole (revision 2.4
// candidate 24): reading gives a version, writing names it in If-Match,
// and a stale one is refused with 409 conflict before anything is
// written.

// versionOf is the version of an object: a hash of its JSON form, read
// with an empty version field.
func versionOf(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return base64.RawURLEncoding.EncodeToString(h[:12])
}

// matches reports whether If-Match names the current version; without
// If-Match the write is not guarded. Quotes and a weak prefix are ignored.
func matches(ifMatch *string, current string) bool {
	if ifMatch == nil {
		return true
	}
	m := strings.Trim(strings.TrimPrefix(strings.TrimSpace(*ifMatch), "W/"), `"`)
	return m == "*" || m == current
}

func errConflict() error {
	return &problem{status: http.StatusConflict, reason: api.ReasonCodeConflict, detail: "changed by someone else since it was read"}
}

// keyLocks serializes check and write per object within the panel, so
// that two writes with the same version cannot both pass.
type keyLocks struct{ m sync.Map }

func (k *keyLocks) lock(key string) func() {
	v, _ := k.m.LoadOrStore(key, &sync.Mutex{})
	mu := v.(*sync.Mutex) //nolint:forcetypeassert // only mutexes are stored
	mu.Lock()
	return mu.Unlock
}

// serverVersion covers the configuration of a server, not its live
// status and runtime data.
func serverVersion(s api.Server) string {
	s.Status, s.StatusSince, s.Runtime, s.Version = "", time.Time{}, nil, ""
	return versionOf(s)
}

// isRCONAdmin: only he changes servers and server groups, so only his
// writes compare versions there; others are refused by role first.
func isRCONAdmin(ctx context.Context) bool {
	p, _ := principal(ctx)
	return p.Role == string(perm.RCONAdmin)
}
