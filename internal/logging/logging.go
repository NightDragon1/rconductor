// SPDX-License-Identifier: AGPL-3.0-or-later

// Package logging builds the structured loggers of panel and agent. Every
// audit-relevant event is one line with a fixed event code and the fields of
// Design 8.2 (NFR-016). Secrets never reach the output on any level (SEC-012).
package logging

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"time"

	"github.com/NightDragon1/rconductor/internal/config"
)

// Field names from Design 8.2.
const (
	KeyTS        = "ts"
	KeyLevel     = "level"
	KeyComp      = "comp"
	KeyEvent     = "event"
	KeyRequestID = "request_id"
	KeySession   = "session"
	KeyUser      = "user"
	KeyRole      = "role"
	KeyServer    = "server"
	KeyBatch     = "batch_id"
	KeyAgent     = "agent"
	KeyReason    = "reason"
	KeyDuration  = "duration_ms"
	KeyVersion   = "version"
)

const tsLayout = "2006-01-02T15:04:05.000Z07:00"

// Factory creates loggers for one application ("panel" or "agent") and its
// modules, all writing to the same handler.
type Factory struct {
	app     string
	handler slog.Handler
}

// New returns a factory writing to w in the format and level from cfg.
func New(w io.Writer, app string, cfg config.Log) *Factory {
	opts := &slog.HandlerOptions{
		Level:       cfg.Level,
		ReplaceAttr: replaceAttr(cfg.Level),
	}
	var h slog.Handler
	if cfg.Format == "json" {
		h = slog.NewJSONHandler(w, opts)
	} else {
		h = slog.NewTextHandler(w, opts)
	}
	return &Factory{app: app, handler: witness(h, opts)}
}

// Root returns the logger of the application itself (comp = app).
func (f *Factory) Root() *slog.Logger {
	return slog.New(f.handler).With(KeyComp, f.app)
}

// Module returns the logger of a module (comp = app.module, e.g. panel.db).
func (f *Factory) Module(name string) *slog.Logger {
	return slog.New(f.handler).With(KeyComp, f.app+"."+name)
}

// Event returns the attribute carrying an event code.
func Event(c Code) slog.Attr { return slog.String(KeyEvent, string(c)) }

// Duration returns the duration_ms attribute. It is dropped unless the
// logger runs on debug (Design 8.2).
func Duration(d time.Duration) slog.Attr {
	return slog.Int64(KeyDuration, d.Milliseconds())
}

// SessionRef returns the short form of a session used in logs: the first
// 8 hex characters of the SHA-256 hash of the session ID (Design 8.2).
func SessionRef(sessionID string) string {
	sum := sha256.Sum256([]byte(sessionID))
	return hex.EncodeToString(sum[:])[:8]
}

func replaceAttr(min slog.Level) func([]string, slog.Attr) slog.Attr {
	return func(groups []string, a slog.Attr) slog.Attr {
		if len(groups) == 0 {
			switch a.Key {
			case slog.TimeKey:
				if a.Value.Kind() == slog.KindTime {
					return slog.String(KeyTS, a.Value.Time().UTC().Format(tsLayout))
				}
			case slog.LevelKey:
				if l, ok := a.Value.Any().(slog.Level); ok {
					return slog.String(KeyLevel, levelName(l))
				}
			case KeyDuration:
				if min > slog.LevelDebug {
					return slog.Attr{}
				}
			}
		}
		return redactAttr(a)
	}
}

func levelName(l slog.Level) string {
	switch {
	case l < slog.LevelInfo:
		return "debug"
	case l < slog.LevelWarn:
		return "info"
	case l < slog.LevelError:
		return "warn"
	default:
		return "error"
	}
}
