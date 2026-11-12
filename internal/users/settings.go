// SPDX-License-Identifier: AGPL-3.0-or-later

package users

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/NightDragon1/rconductor/internal/agentview"
	"github.com/NightDragon1/rconductor/internal/auth"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/profiles"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/sqlitedb"
)

// Settings keys of the WebSocket limits and their defaults (SEC-008,
// revision 2.2 candidate 10).
const (
	SettingWSMaxSockets = "ws.max_sockets_per_session"
	SettingWSMaxSubs    = "ws.max_subscriptions_per_socket"
	DefaultWSMaxSockets = 20
	DefaultWSMaxSubs    = 16
)

// SettingConnectionTests limits tests of RCON data per user and minute
// (SRV-007).
const (
	SettingConnectionTests = "servers.connection_test_per_minute"
	DefaultConnectionTests = 10
)

// Requests at once to one game server with an HTTPS API (PROT-003,
// D-071); RCON keeps its one connection (PROT-002).
const (
	SettingHTTPParallel = "servers.http_parallel"
	DefaultHTTPParallel = 4
	MinHTTPParallel     = 1
)

// SettingShortcuts switches keyboard shortcuts on for everyone (UI-008);
// off by default, readable by all through the session (revision 2.4
// candidate 7).
const SettingShortcuts = "ui.shortcuts_enabled"

// SettingFingerprintPairing opens pairing by fingerprint (way B, AGT-005);
// off by default, it closes itself after PairingWindow (revision 2.3
// candidate 3).
const (
	SettingFingerprintPairing = "agents.fingerprint_pairing"
	PairingWindow             = 30 * time.Minute
)

// Settings key of the audit retention and its default (AUD-003).
const (
	SettingAuditRetentionDays = "audit.retention_days"
	DefaultAuditRetentionDays = 90
	MinAuditRetentionDays     = 30
)

// Settings of the mail (NFR-017): the time for one delivery and the test
// mails per user and minute.
const (
	SettingSMTPTimeout       = "smtp.timeout"
	DefaultSMTPTimeout       = 10 * time.Second
	MinSMTPTimeout           = time.Second
	SettingSMTPTestPerMinute = "smtp.test_per_minute"
	DefaultSMTPTestPerMinute = 5
	MinSMTPTestPerMinute     = 1
)

// Command templates per server (CMD-008, D-068).
const (
	SettingTemplatesPerServer = "commands.templates_per_server"
	DefaultTemplatesPerServer = 100
	MinTemplatesPerServer     = 1
)

// Commands to several servers at once (CMD-010, D-069): servers per call
// and runs at the same time.
const (
	SettingMultiMaxServers = "commands.multi_max_servers"
	DefaultMultiMaxServers = 50
	MinMultiMaxServers     = 1
	SettingMultiParallel   = "commands.multi_parallel"
	DefaultMultiParallel   = 8
	MinMultiParallel       = 1
)

// rule checks one setting value; it returns the value to store.
type rule func(raw json.RawMessage) (any, error)

func durationRule(lo, hi time.Duration) rule {
	return func(raw json.RawMessage) (any, error) {
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, fmt.Errorf("a duration such as \"24h\"")
		}
		d, err := time.ParseDuration(v)
		if err != nil || d < lo || d > hi {
			return nil, fmt.Errorf("a duration from %s to %s", lo, hi)
		}
		return d.String(), nil
	}
}

func intRule(lo, hi int) rule {
	return func(raw json.RawMessage) (any, error) {
		var n int
		if err := json.Unmarshal(raw, &n); err != nil || n < lo || n > hi {
			return nil, fmt.Errorf("a whole number from %d to %d", lo, hi)
		}
		return n, nil
	}
}

func boolRule(raw json.RawMessage) (any, error) {
	var b bool
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, fmt.Errorf("true or false")
	}
	return b, nil
}

// settingRules are the global settings the RCON admin may change (Design
// 5.1); any other key is refused.
var settingRules = map[string]rule{
	"backup.interval":             durationRule(time.Hour, 30*24*time.Hour),
	"backup.keep_auto":            intRule(1, 1000),
	"backup.keep_pre_migration":   intRule(1, 1000),
	"session.idle_timeout":        durationRule(time.Minute, 30*24*time.Hour),
	"session.max_lifetime":        durationRule(time.Minute, 90*24*time.Hour),
	"login.max_failures":          intRule(1, 100),
	"login.max_client_failures":   intRule(1, 1000),
	"login.max_tracked":           intRule(1000, 1000000),
	"login.lockout_duration":      durationRule(time.Minute, 24*time.Hour),
	"login.overflow_gap":          durationRule(50*time.Millisecond, 10*time.Second),
	"login.overflow_line":         intRule(1, 1000),
	SettingInviteLifetime:         durationRule(time.Minute, 30*24*time.Hour),
	perm.SettingSupportersEnabled: boolRule,
	SettingAuditRetentionDays:     intRule(MinAuditRetentionDays, 3650),
	SettingWSMaxSockets:           intRule(1, 100),
	SettingWSMaxSubs:              intRule(1, 64),
	profiles.SettingPollInterval:  durationRule(profiles.MinPollInterval, time.Hour),
	SettingFingerprintPairing:     boolRule,
	SettingShortcuts:              boolRule,
	SettingConnectionTests:        intRule(1, 120),
	SettingHTTPParallel:           intRule(MinHTTPParallel, 64),
	// Version search and volume sizes (AGT-006, AGT-007).
	agentview.SettingVersionRetry:   durationRule(agentview.MinVersionRetry, time.Hour),
	agentview.SettingVersionWindow:  durationRule(agentview.MinVersionWindow, 24*time.Hour),
	agentview.SettingVolumeInterval: durationRule(agentview.MinVolumeInterval, 7*24*time.Hour),
	agentview.SettingVolumeStale:    durationRule(agentview.MinVolumeStale, 30*24*time.Hour),
	// Second factor (AUTH-008).
	auth.SettingTOTPRequired:       boolRule,
	auth.SettingSecondFactorWindow: durationRule(auth.MinSecondFactorWindow, time.Hour),
	auth.SettingRecoveryLifetime:   durationRule(auth.MinRecoveryLifetime, 24*time.Hour),
	auth.SettingRecoveryPerHour:    intRule(auth.MinRecoveryPerHour, 100),
	auth.SettingRecoveryWorkers:    intRule(auth.MinRecoveryWorkers, 64),
	auth.SettingRecoveryQueue:      intRule(auth.MinRecoveryQueue, 10000),
	SettingSMTPTimeout:             durationRule(MinSMTPTimeout, 5*time.Minute),
	SettingSMTPTestPerMinute:       intRule(MinSMTPTestPerMinute, 60),
	SettingTemplatesPerServer:      intRule(MinTemplatesPerServer, 1000),
	SettingMultiMaxServers:         intRule(MinMultiMaxServers, 1000),
	SettingMultiParallel:           intRule(MinMultiParallel, 64),
}

// Rate limits per role (Design 3.4, 5.6): ratelimit.<role>.per_minute and
// ratelimit.<role>.burst; the defaults come from the command pipeline.
func init() {
	for _, r := range perm.Roles {
		settingRules["ratelimit."+string(r)+".per_minute"] = intRule(1, 10000)
		settingRules["ratelimit."+string(r)+".burst"] = intRule(1, 1000)
	}
}

// EnsureAuditDefaults stores the default audit retention unless it exists.
func EnsureAuditDefaults(ctx context.Context, st *store.Store) error {
	for key, v := range map[string]int{
		SettingAuditRetentionDays: DefaultAuditRetentionDays,
		SettingWSMaxSockets:       DefaultWSMaxSockets,
		SettingWSMaxSubs:          DefaultWSMaxSubs,
		SettingConnectionTests:    DefaultConnectionTests,
		SettingTemplatesPerServer: DefaultTemplatesPerServer,
		SettingMultiMaxServers:    DefaultMultiMaxServers,
		SettingMultiParallel:      DefaultMultiParallel,
		SettingHTTPParallel:       DefaultHTTPParallel,
	} {
		if _, err := st.EnsureDefault(ctx, key, v); err != nil {
			return err
		}
	}
	for _, key := range []string{SettingFingerprintPairing, SettingShortcuts} {
		if _, err := st.EnsureDefault(ctx, key, false); err != nil {
			return err
		}
	}
	return nil
}

// Settings returns the global settings; only the RCON admin reads them.
func (s *Service) Settings(ctx context.Context, req Request) (map[string]json.RawMessage, error) {
	if req.Actor.Role != perm.RCONAdmin {
		s.deniedOn(ctx, req, "settings.read", "settings", "", "forbidden", nil)
		return nil, ErrForbidden
	}
	rows, err := s.st.Queries.ListGlobalSettings(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]json.RawMessage{}
	for _, r := range rows {
		out[r.Key] = json.RawMessage(r.Value)
	}
	return out, nil
}

// SettingsVersion is the version of the global settings, for If-Match.
func (s *Service) SettingsVersion(ctx context.Context) (int64, error) {
	return s.st.Queries.GetSettingsVersion(ctx)
}

// UpdateSettings changes global settings; only the RCON admin, only known
// keys with valid values, all or nothing (AUD-003, AUD-004). Switching
// supporters off ends the sessions of all supporters (decision W2).
func (s *Service) UpdateSettings(ctx context.Context, req Request, in map[string]json.RawMessage, version *int64) (map[string]json.RawMessage, error) {
	if req.Actor.Role != perm.RCONAdmin {
		keysTried := make([]string, 0, len(in))
		for k := range in {
			keysTried = append(keysTried, k)
		}
		s.deniedOn(ctx, req, "settings.update", "settings", "", "forbidden", map[string]any{"keys": keysTried})
		return nil, ErrForbidden
	}
	values := map[string]string{}
	for k, raw := range in {
		r, ok := settingRules[k]
		if !ok {
			return nil, &InvalidError{Detail: fmt.Sprintf("unknown setting %q", k)}
		}
		v, err := r(raw)
		if err != nil {
			return nil, &InvalidError{Detail: fmt.Sprintf("%s must be %s", k, err)}
		}
		b, _ := json.Marshal(v)
		values[k] = string(b)
	}
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	q := sqlitedb.New(tx)
	// Optimistic locking in one statement (revision 2.4 candidate 24);
	// the version is required, checked after the role.
	if version == nil {
		return nil, ErrPreconditionRequired
	}
	n, err := q.BumpSettingsVersion(ctx, *version)
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, ErrConflict
	}
	// Compared with the settings of the version the caller read, in the
	// transaction.
	rows, err := q.ListGlobalSettings(ctx)
	if err != nil {
		return nil, err
	}
	cur := map[string]json.RawMessage{}
	for _, r := range rows {
		cur[r.Key] = json.RawMessage(r.Value)
	}
	changes := map[string]any{}
	for k, v := range values {
		if string(cur[k]) == v {
			continue
		}
		if err := q.SetGlobalSetting(ctx, sqlitedb.SetGlobalSettingParams{Value: v, UpdatedAt: store.Now(), Key: k}); err != nil {
			return nil, err
		}
		changes[k] = map[string]json.RawMessage{"from": cur[k], "to": json.RawMessage(v)}
	}
	if len(changes) > 0 {
		if err := s.auditOn(ctx, tx, req, "settings.update", "settings", "", changes); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	if _, ok := changes[perm.SettingSupportersEnabled]; ok {
		sups, err := s.st.Queries.ListSupporterIDs(ctx)
		if err != nil {
			return nil, err
		}
		if values[perm.SettingSupportersEnabled] == "false" {
			if err := s.rightsChanged(ctx, req, "settings.update", sups...); err != nil {
				return nil, err
			}
		}
	}
	s.perm.Invalidate()
	return s.Settings(ctx, req)
}
