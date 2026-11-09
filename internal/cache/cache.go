// SPDX-License-Identifier: AGPL-3.0-or-later

// Package cache holds runtime data of game servers in memory only, with
// their age (NFR-005, SRV-006, Overview 4.6). Nothing here is stored in
// the database; after a restart the data is fetched again. Old values are
// shown as stale, never hidden silently.
package cache

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/NightDragon1/rconductor/internal/connmgr"
	"github.com/NightDragon1/rconductor/internal/foreign"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/pattern"
	"github.com/NightDragon1/rconductor/internal/profiles"
	"github.com/NightDragon1/rconductor/internal/rcon"
	"github.com/NightDragon1/rconductor/internal/rcon/httpapi"
	"github.com/NightDragon1/rconductor/internal/store"
)

// Timing of the player list (Overview 4.6).
const (
	PlayersRefresh = 60 * time.Second
	PlayersStale   = 120 * time.Second
	fetchTimeout   = 10 * time.Second
)

// Limits of the player list as foreign data (SEC-006).
const (
	MaxPlayers    = 500
	MaxPlayerName = 64
	// maxCount bounds a count or limit a game reports.
	maxCount = 1_000_000
)

// ErrNoPlayerList means the server's profile defines no player list.
var ErrNoPlayerList = errors.New("the game profile has no player list")

var errNoMatch = errors.New("the answer does not fit the player pattern")

// Executor runs a command on a server's connection.
type Executor interface {
	Execute(ctx context.Context, serverID, command string) (string, error)
}

// PlayerReader asks a server's connection for its players without a
// console command (protocol http-api, PROT-003); the connection manager
// implements it next to Executor.
type PlayerReader interface {
	Players(ctx context.Context, serverID string) (rcon.Players, error)
}

// Players is a cached player list.
type Players struct {
	Names     []string
	UpdatedAt time.Time
	// Stale is set when the value is older than PlayersStale or the RCON
	// connection was lost since (Overview 4.6).
	Stale bool
	// Unknown is set when the answer did not fit the profile's pattern;
	// Names is empty then and says nothing about the players online.
	Unknown bool
	// Count is the number online; for a game without names (protocol
	// http-api) the only value. Max is the limit, 0 if unknown.
	Count int
	Max   int
}

type entry struct {
	players     Players
	invalidated bool
	viewers     int
	fetching    chan struct{} // closed when a running fetch ends
	stop        chan struct{} // ends the refresh loop
}

// Cache keeps runtime data per server.
type Cache struct {
	st   *store.Store
	exec Executor
	log  *slog.Logger
	now  func() time.Time

	mu      sync.Mutex
	entries map[string]*entry
	// refresh is PlayersRefresh unless set otherwise, stale fits it.
	refresh, stale time.Duration
	// onPlayers is told about every new player list.
	onPlayers func(serverID string)
}

// OnPlayers sets the receiver of new player lists, for the event bus.
func (c *Cache) OnPlayers(f func(serverID string)) { c.onPlayers = f }

// Cached returns the player list of a server as it is, without asking the
// game server; ok is false when there is none yet.
func (c *Cache) Cached(serverID string) (Players, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.entries[serverID]
	if e == nil || e.players.UpdatedAt.IsZero() {
		return Players{}, false
	}
	return c.view(e), true
}

// New returns an empty cache.
func New(st *store.Store, exec Executor, log *slog.Logger) *Cache {
	return &Cache{st: st, exec: exec, log: log, now: time.Now, entries: map[string]*entry{}, refresh: PlayersRefresh, stale: PlayersStale}
}

// SetRefresh replaces PlayersRefresh; zero keeps it. A value counts as stale
// after two refreshes that did not come, and never before PlayersStale.
func (c *Cache) SetRefresh(d time.Duration) {
	if d > 0 {
		c.refresh = d
		c.stale = max(PlayersStale, 2*d)
	}
}

func (c *Cache) get(id string) *entry {
	e := c.entries[id]
	if e == nil {
		e = &entry{}
		c.entries[id] = e
	}
	return e
}

// Invalidate marks the data of a server as stale, for example when its
// RCON connection is lost or the server restarts; cache.stale is logged.
func (c *Cache) Invalidate(serverID, reason string) {
	c.mu.Lock()
	e := c.entries[serverID]
	had := e != nil && !e.players.UpdatedAt.IsZero() && !e.invalidated
	if e != nil {
		e.invalidated = true
	}
	c.mu.Unlock()
	if had {
		c.log.Info("cached data is stale", logging.Event(logging.CacheStale),
			slog.String(logging.KeyServer, serverID), slog.String("data", "players"), slog.String(logging.KeyReason, reason))
	}
}

// Players returns the player list of a server: cached, or fetched now if
// missing, stale or invalidated. If the fetch fails, the last value comes
// back marked stale.
func (c *Cache) Players(ctx context.Context, serverID string) (Players, error) {
	c.mu.Lock()
	e := c.get(serverID)
	p := c.view(e)
	fresh := !p.UpdatedAt.IsZero() && !p.Stale
	c.mu.Unlock()
	if fresh {
		return p, nil
	}
	if err := c.fetch(ctx, serverID); err != nil {
		c.mu.Lock()
		p := c.view(c.get(serverID))
		c.mu.Unlock()
		if !p.UpdatedAt.IsZero() {
			p.Stale = true
			return p, nil
		}
		return Players{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.view(c.get(serverID)), nil
}

func (c *Cache) view(e *entry) Players {
	p := e.players
	p.Names = append([]string(nil), p.Names...)
	p.Stale = e.invalidated || (!p.UpdatedAt.IsZero() && c.now().Sub(p.UpdatedAt) > c.stale)
	return p
}

// fetch asks the game server; concurrent callers share one fetch.
func (c *Cache) fetch(ctx context.Context, serverID string) error {
	c.mu.Lock()
	e := c.get(serverID)
	if wait := e.fetching; wait != nil {
		c.mu.Unlock()
		select {
		case <-wait:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	done := make(chan struct{})
	e.fetching = done
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		e.fetching = nil
		c.mu.Unlock()
		close(done)
	}()

	cfg, protocol, err := c.playerConfig(ctx, serverID)
	if err != nil {
		return err
	}
	fctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	var p Players
	if cfg == nil {
		// No players block: a game with an HTTPS API tells its players
		// through the dialect, with or without names (PROT-003).
		pr, ok := c.exec.(PlayerReader)
		if !ok || protocol != httpapi.Protocol {
			return ErrNoPlayerList
		}
		got, err := pr.Players(fctx, serverID)
		if errors.Is(err, connmgr.ErrNoPlayerCounter) {
			return ErrNoPlayerList
		}
		if err != nil {
			return err
		}
		p = fromGame(got)
	} else {
		answer, err := c.exec.Execute(fctx, serverID, cfg.Command)
		if err != nil {
			return err
		}
		names, err := parsePlayers(fctx, answer, cfg)
		unknown := errors.Is(err, errNoMatch)
		if err != nil && !unknown {
			return err
		}
		p = Players{Names: names, Unknown: unknown, Count: len(names)}
	}
	unknown := p.Unknown
	p.UpdatedAt = c.now()
	c.mu.Lock()
	first := unknown && !e.players.Unknown
	e.players = p
	e.invalidated = false
	c.mu.Unlock()
	if first {
		c.log.Warn("the answer does not fit the player pattern of the profile", slog.String(logging.KeyServer, serverID))
	}
	if c.onPlayers != nil {
		c.onPlayers(serverID)
	}
	return nil
}

// playerConfig is the players block of the server's profile, nil if it
// has none, and the server's protocol.
func (c *Cache) playerConfig(ctx context.Context, serverID string) (*profiles.Players, string, error) {
	r, err := c.st.Queries.GetServer(ctx, serverID)
	if err != nil || !r.ProfileID.Valid {
		return nil, "", ErrNoPlayerList
	}
	// The players block of the topmost profile that has one (D-074).
	rules, _, err := profiles.ForServer(ctx, c.st.Queries, serverID)
	if err != nil || rules == nil {
		return nil, "", ErrNoPlayerList
	}
	return rules.Players, r.Protocol, nil
}

// fromGame takes the players a game reported through its API; count,
// limit and names are foreign data, bounded like a parsed list (SEC-006).
func fromGame(g rcon.Players) Players {
	p := Players{Count: min(max(g.Count, 0), maxCount), Max: g.Max, Names: []string{}}
	if p.Max < 1 || p.Max > maxCount {
		p.Max = 0
	}
	for _, raw := range g.Names {
		if name := foreign.Name(raw, MaxPlayerName); name != "" {
			p.Names = append(p.Names, name)
			if len(p.Names) == MaxPlayers {
				break
			}
		}
	}
	return p
}

// parsePlayers takes the first capture group of the profile's pattern and
// splits it at the separator; names are foreign data, cleaned and bounded
// in length and count (SEC-006). When the group is empty, the names are on
// the next line, as old Minecraft versions print them. An answer the
// pattern does not fit gives errNoMatch: the number is unknown, not 0.
func parsePlayers(ctx context.Context, answer string, cfg *profiles.Players) ([]string, error) {
	p, err := pattern.Compile(cfg.Pattern, 1)
	if err != nil {
		return nil, err
	}
	m, next, ok, err := p.FirstMatchNext(ctx, answer)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errNoMatch
	}
	names := []string{}
	list := m[0]
	if strings.TrimSpace(list) == "" {
		list = next
	}
	if strings.TrimSpace(list) == "" {
		return names, nil
	}
	sep := cfg.Separator
	if sep == "" {
		sep = ","
	}
	for raw := range strings.SplitSeq(list, sep) {
		name := foreign.Name(raw, MaxPlayerName)
		if name == "" {
			continue
		}
		names = append(names, name)
		if len(names) == MaxPlayers {
			break
		}
	}
	return names, nil
}

// Watch tells the cache that someone views a server; while anyone does,
// the player list is refreshed every 60 s (Overview 4.6). The returned
// function ends the view.
func (c *Cache) Watch(ctx context.Context, serverID string) func() {
	c.mu.Lock()
	e := c.get(serverID)
	e.viewers++
	if e.viewers == 1 {
		e.stop = make(chan struct{})
		go c.refreshLoop(ctx, serverID, e.stop)
	}
	c.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			defer c.mu.Unlock()
			e.viewers--
			if e.viewers == 0 && e.stop != nil {
				close(e.stop)
				e.stop = nil
			}
		})
	}
}

// refreshLoop fetches the player list at once for the first viewer and
// then every refresh interval while someone views (Gesamtkonzept 4.6).
func (c *Cache) refreshLoop(ctx context.Context, serverID string, stop chan struct{}) {
	t := time.NewTicker(c.refresh)
	defer t.Stop()
	for {
		if err := c.fetch(ctx, serverID); err != nil && !errors.Is(err, ErrNoPlayerList) {
			c.log.Debug("player list not refreshed", slog.String(logging.KeyServer, serverID), slog.Any("err", err))
		}
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-t.C:
		}
	}
}
