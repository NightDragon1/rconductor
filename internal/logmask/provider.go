// SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
// SPDX-License-Identifier: AGPL-3.0-or-later

package logmask

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/NightDragon1/rconductor/internal/profiles"
)

// maxCompiled bounds the cache of compiled profile patterns; a server has
// one entry, so this is far above any real number of servers with patterns.
const maxCompiled = 1024

// Provider masks batches of log lines with the settings of the RCON admin
// as last read (UI-022, D-088, F-93): a read error keeps the last state;
// without any readable state since the start, list and profiles mask and
// the texts are missing.
type Provider struct {
	settings *Settings
	log      *slog.Logger

	mu       sync.RWMutex
	enabled  bool
	budget   time.Duration
	base     *Masker // the texts of the admin; nil masks the list only
	compiled map[string]*Masker
}

// NewProvider reads the settings once and again after every change.
func NewProvider(ctx context.Context, s *Settings, log *slog.Logger) *Provider {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	p := &Provider{settings: s, log: log, enabled: true, budget: DefaultBudget, compiled: map[string]*Masker{}}
	p.Reload(ctx)
	s.OnChange(func() { p.Reload(context.Background()) })
	return p
}

// Reload reads the settings; on a read error the last state stays, and
// Load or OpenTexts has logged it.
func (p *Provider) Reload(ctx context.Context) {
	st, err := p.settings.Load(ctx)
	if err != nil {
		return
	}
	texts, err := p.settings.OpenTexts(st)
	if err != nil {
		// The switch and the budget are readable: protection on as stored,
		// the texts as last read.
		p.mu.Lock()
		p.enabled, p.budget = st.Enabled, time.Duration(st.BudgetMS)*time.Millisecond
		p.mu.Unlock()
		return
	}
	// Stored texts are not checked against the limits when loaded.
	base, err := New(texts, Limits{Texts: MaxTexts, TextRunes: MaxTextRunes})
	if err != nil {
		p.log.Error("masking texts not usable", slog.Int("texts", len(texts)))
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.enabled, p.budget, p.base = st.Enabled, time.Duration(st.BudgetMS)*time.Millisecond, base
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

// masker is the masker of the texts with the patterns of the profiles,
// compiled once per set of patterns.
func (p *Provider) masker(profilePatterns [][]string) *Masker {
	var key strings.Builder
	for _, ps := range profilePatterns {
		for _, pat := range ps {
			key.WriteString(pat)
			key.WriteByte(0)
		}
		key.WriteByte(1)
	}
	p.mu.RLock()
	m, ok := p.compiled[key.String()]
	base := p.base
	p.mu.RUnlock()
	if ok {
		return m
	}
	m = base
	for _, ps := range profilePatterns {
		res, bad := profiles.CompileLogMasks(ps)
		if len(bad) > 0 {
			p.log.Warn("profile masking patterns left out", slog.Any("positions", bad))
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
