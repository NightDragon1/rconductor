// SPDX-License-Identifier: AGPL-3.0-or-later

package logging

// Code is a fixed event code (Design 8.3). Codes are an interface: within a
// major version they are only added, never renamed or removed (NFR-016).
type Code string

// Operation.
const (
	AppStart        Code = "app.start"
	AppReady        Code = "app.ready"
	DBMigrateStart  Code = "db.migrate.start"
	DBMigrateDone   Code = "db.migrate.done"
	DBMigrateLockWt Code = "db.migrate.lock_wait"
	KeyCreated      Code = "key.created"
	KeyLoaded       Code = "key.loaded"
	BackupDone      Code = "backup.done"
	BackupFailed    Code = "backup.failed"
	AuditPurge      Code = "audit.purge"
)

// Authentication.
const (
	AuthLoginOK         Code = "auth.login.ok"
	AuthLoginFailed     Code = "auth.login.failed"
	AuthLockout         Code = "auth.lockout"
	AuthLogout          Code = "auth.logout"
	AuthSessionRevoked  Code = "auth.session.revoked"
	AuthInitialPassword Code = "auth.initial_password.issued" //nolint:gosec // event code, not a credential
	AuthInviteUsed      Code = "auth.invite.used"
	// AuthEmergencyReset is new in revision 2.1 (AUTH-011).
	AuthEmergencyReset Code = "auth.emergency_admin.reset"
)

// Permissions.
const (
	PermDenied     Code = "perm.denied"
	PermRecomputed Code = "perm.recomputed"
)

// Commands.
const (
	CmdAccepted Code = "cmd.accepted"
	CmdDenied   Code = "cmd.denied"
	CmdSent     Code = "cmd.sent"
	CmdResponse Code = "cmd.response"
	CmdTimeout  Code = "cmd.timeout"
)

// RCON.
const (
	RCONConnected    Code = "rcon.connected"
	RCONAuthFailed   Code = "rcon.auth_failed"
	RCONDisconnected Code = "rcon.disconnected"
	RCONReconnect    Code = "rcon.reconnect"
)

// Game profiles.
const (
	ProfileImported   Code = "profile.imported"
	ProfileRejected   Code = "profile.rejected"
	ProfileOverridden Code = "profile.overridden"
)

// Agent as seen by the panel.
const (
	AgentConnected Code = "agent.connected"
	AgentRejected  Code = "agent.rejected"
	AgentPaired    Code = "agent.paired"
	AgentRevoked   Code = "agent.revoked"
	AgentLimit     Code = "agent.limit"
	// AgentDisconnected is new in revision 2.3 candidate 5.
	AgentDisconnected Code = "agent.disconnected"
)

// Agent itself.
const (
	AgentHandshake   Code = "agent.handshake"
	AgentForbidden   Code = "agent.forbidden"
	AgentUnsupported Code = "agent.unsupported"
	DockerAction     Code = "docker.action"
	LogsFollowStart  Code = "logs.follow.start"
	LogsFollowStop   Code = "logs.follow.stop"
)

// Live data.
const (
	WSSubscribe       Code = "ws.subscribe"
	WSSubscribeDenied Code = "ws.subscribe.denied"
	CacheStale        Code = "cache.stale"
)

// AllCodes lists every defined code, in the order of Design 8.3.
var AllCodes = []Code{
	AppStart, AppReady, DBMigrateStart, DBMigrateDone, DBMigrateLockWt,
	KeyCreated, KeyLoaded, BackupDone, BackupFailed, AuditPurge,
	AuthLoginOK, AuthLoginFailed, AuthLockout, AuthLogout, AuthSessionRevoked,
	AuthInitialPassword, AuthInviteUsed, AuthEmergencyReset,
	PermDenied, PermRecomputed,
	CmdAccepted, CmdDenied, CmdSent, CmdResponse, CmdTimeout,
	RCONConnected, RCONAuthFailed, RCONDisconnected, RCONReconnect,
	ProfileImported, ProfileRejected, ProfileOverridden,
	AgentConnected, AgentRejected, AgentPaired, AgentRevoked, AgentLimit, AgentDisconnected,
	AgentHandshake, AgentForbidden, AgentUnsupported, DockerAction,
	LogsFollowStart, LogsFollowStop,
	WSSubscribe, WSSubscribeDenied, CacheStale,
}
