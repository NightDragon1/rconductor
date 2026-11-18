// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package logmask

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/profiles"
)

// maxCompiled bounds the cache of compiled profile patterns; a server has
// one entry, so this is far above any real number of servers with patterns.
const maxCompiled = 1024

// Provider masks batches of log lines with the settings of the RCON admin
// as last read (UI-022, D-088, F-93): on a read error masking is on and
// budget and texts stay as last read; without any readable state since the
// start, list and profiles mask and the texts are missing.
type Provider struct {
	settings *Settings
	log      *slog.Logger

	// reload lets one reload at a time read and take over; each reads under
	// it, so the last one to run holds the newest state, also after a repair
	// that set a lower version.
	reload sync.Mutex

	mu       sync.RWMutex
	enabled  bool
	budget   time.Duration
	base     *Masker // the texts of the admin; nil masks the list only
	compiled map[string]*Masker
	// servers holds the profile patterns of each server as last read.
	servers map[string][][]string
}

// NewProvider reads the settings once and again after every change.
func NewProvider(ctx context.Context, s *Settings, log *slog.Logger) *Provider {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	p := &Provider{settings: s, log: log, enabled: true, budget: DefaultBudget, compiled: map[string]*Masker{}, servers: map[string][][]string{}}
	s.OnChange(func() { p.Reload(context.Background()) }) // before the first read: no change is missed
	p.Reload(ctx)
	return p
}

// Reload reads the settings; on a read error masking is on and budget and
// texts stay as last read, and Load or OpenTexts has logged it.
func (p *Provider) Reload(ctx context.Context) {
	p.reload.Lock()
	defer p.reload.Unlock()
	st, err := p.settings.Load(ctx)
	if errors.Is(err, ErrUnreadable) {
		// A switch that turns protection off counts as on when it cannot be
		// read (F-93); budget and texts stay as last read.
		p.mu.Lock()
		p.enabled = true
		p.mu.Unlock()
		return
	}
	if err != nil {
		return // canceled: nothing read, nothing changes
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.enabled, p.budget = st.Enabled, time.Duration(st.BudgetMS)*time.Millisecond
	texts, err := p.settings.OpenTexts(st)
	if err != nil {
		return // switch and budget as stored, the texts as last read
	}
	// Stored texts are not checked against the limits when loaded.
	base, err := New(texts, Limits{Texts: MaxTexts, TextRunes: MaxTextRunes})
	if err != nil {
		p.log.Error("masking texts not usable", logging.Event(logging.SettingReadFailed), slog.Int("texts", len(texts)))
		return
	}
	p.base = base
	clear(p.compiled)
}

// Lines masks one batch of lines of a server whose profiles have the given
// masking patterns, top profile first; switched off, the lines pass as
// they are. The number of lines withheld for the time budget comes back.
func (p *Provider) Lines(lines []string, profilePatterns [][]string) ([]string, int) {
	p.mu.RLock()
	enabled, budget := p.enabled, p.budget
	p.mu.RUnlock()
	if !enabled {
		return lines, 0
	}
	return p.masker(profilePatterns).Lines(lines, Budget{Max: budget})
}

// ServerLines masks one batch of lines of a server with the patterns of
// its profiles; when they cannot be read, the patterns as last read hold,
// without any, list and texts (F-93). Withheld lines are logged.
func (p *Provider) ServerLines(ctx context.Context, serverID string, lines []string) []string {
	patterns, err := profiles.LogMasksForServer(ctx, p.settings.st.Queries, serverID)
	p.mu.Lock()
	if err != nil {
		patterns = p.servers[serverID]
	} else {
		if len(p.servers) >= maxCompiled {
			clear(p.servers)
		}
		p.servers[serverID] = patterns
	}
	p.mu.Unlock()
	if err != nil && ctx.Err() == nil {
		p.log.Error("profile masking patterns not readable", logging.Event(logging.SettingReadFailed), slog.String(logging.KeyServer, serverID))
	}
	out, withheld := p.Lines(lines, patterns)
	if withheld > 0 {
		p.log.Warn("log lines withheld: masking took too long", slog.String(logging.KeyServer, serverID), slog.Int("lines", withheld))
	}
	return out
}

// ListLines masks with the built-in list alone, for a way out of the panel
// that has no provider (fail closed).
func ListLines(lines []string) []string {
	out, _ := (*Masker)(nil).WithPatterns(nil).Lines(lines, Budget{Max: DefaultBudget})
	return out
}

// masker is the masker of the texts with the patterns of the profiles,
// compiled once per set of patterns.
func (p *Provider) masker(profilePatterns [][]string) *Masker {
	var key strings.Builder // each pattern with its length, so no two sets meet
	for _, ps := range profilePatterns {
		for _, pat := range ps {
			key.WriteString(strconv.Itoa(len(pat)))
			key.WriteByte(':')
			key.WriteString(pat)
		}
		key.WriteByte('|')
	}
	p.mu.RLock()
	m, ok := p.compiled[key.String()]
	base := p.base
	p.mu.RUnlock()
	if ok {
		return m
	}
	m = base
	for i, ps := range profilePatterns {
		res, bad := profiles.CompileLogMasks(ps)
		if len(bad) > 0 {
			p.log.Warn("profile masking patterns left out", logging.Event(logging.SettingReadFailed), slog.Int("profile", i), slog.Any("positions", bad))
		}
		m = m.WithPatterns(res)
	}
	if m == nil {
		m = (*Masker)(nil).WithPatterns(nil)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.base == base {
		if len(p.compiled) >= maxCompiled {
			clear(p.compiled)
		}
		p.compiled[key.String()] = m
	}
	return m
}
