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
	AuditLost       Code = "audit.entry_lost"
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
	// Second factor (AUTH-008): switched on, switched off (also by reset or
	// recovery, with the cause), a recovery link requested and used.
	AuthTOTPEnabled       Code = "auth.totp.enabled"
	AuthTOTPDisabled      Code = "auth.totp.disabled"
	AuthRecoveryRequested Code = "auth.recovery.requested"
	AuthRecoveryUsed      Code = "auth.recovery.used"
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
	// RCONConnectionTest is a test of RCON data (SRV-007), with its result.
	RCONConnectionTest Code = "rcon.connection_test"
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
	// LogsTail: earlier log lines read over the agent (revision 2.4
	// candidate 20).
	LogsTail Code = "logs.tail"
)

// Live data.
const (
	WSSubscribe       Code = "ws.subscribe"
	WSSubscribeDenied Code = "ws.subscribe.denied"
	CacheStale        Code = "cache.stale"
)

// Mail (NFR-017): only the recipient's domain and the reason as a
// category (auth, tls, connect, timeout, rejected), never an address,
// password or server answer.
const (
	MailSent   Code = "mail.sent"
	MailFailed Code = "mail.failed"
)

// AllCodes lists every defined code, in the order of Design 8.3.
var AllCodes = []Code{
	AppStart, AppReady, DBMigrateStart, DBMigrateDone, DBMigrateLockWt,
	KeyCreated, KeyLoaded, BackupDone, BackupFailed, AuditPurge, AuditLost,
	AuthLoginOK, AuthLoginFailed, AuthLockout, AuthLogout, AuthSessionRevoked,
	AuthInitialPassword, AuthInviteUsed, AuthEmergencyReset,
	AuthTOTPEnabled, AuthTOTPDisabled, AuthRecoveryRequested, AuthRecoveryUsed,
	PermDenied, PermRecomputed,
	CmdAccepted, CmdDenied, CmdSent, CmdResponse, CmdTimeout,
	RCONConnected, RCONAuthFailed, RCONDisconnected, RCONReconnect, RCONConnectionTest,
	ProfileImported, ProfileRejected, ProfileOverridden,
	AgentConnected, AgentRejected, AgentPaired, AgentRevoked, AgentLimit, AgentDisconnected,
	AgentHandshake, AgentForbidden, AgentUnsupported, DockerAction,
	LogsFollowStart, LogsFollowStop, LogsTail,
	WSSubscribe, WSSubscribeDenied, CacheStale,
	MailSent, MailFailed,
}
