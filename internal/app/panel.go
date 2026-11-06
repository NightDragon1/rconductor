// SPDX-License-Identifier: AGPL-3.0-or-later

// Package app wires the modules of panel and agent and runs their start-up
// sequence (Architecture 5.4).
package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/NightDragon1/rconductor/internal/agenthub"
	agentadmin "github.com/NightDragon1/rconductor/internal/agents"
	"github.com/NightDragon1/rconductor/internal/agentview"
	"github.com/NightDragon1/rconductor/internal/auth"
	"github.com/NightDragon1/rconductor/internal/auth/password"
	"github.com/NightDragon1/rconductor/internal/buildinfo"
	"github.com/NightDragon1/rconductor/internal/cache"
	"github.com/NightDragon1/rconductor/internal/commands"
	"github.com/NightDragon1/rconductor/internal/config"
	"github.com/NightDragon1/rconductor/internal/connmgr"
	eventbus "github.com/NightDragon1/rconductor/internal/events"
	"github.com/NightDragon1/rconductor/internal/logging"
	"github.com/NightDragon1/rconductor/internal/netlimit"
	"github.com/NightDragon1/rconductor/internal/ops/backup"
	"github.com/NightDragon1/rconductor/internal/ops/cleanup"
	"github.com/NightDragon1/rconductor/internal/ops/keys"
	"github.com/NightDragon1/rconductor/internal/perm"
	"github.com/NightDragon1/rconductor/internal/profiles"
	"github.com/NightDragon1/rconductor/internal/rcon"
	"github.com/NightDragon1/rconductor/internal/rcon/source"
	"github.com/NightDragon1/rconductor/internal/servers"
	"github.com/NightDragon1/rconductor/internal/store"
	"github.com/NightDragon1/rconductor/internal/store/secret"
	"github.com/NightDragon1/rconductor/internal/users"
	"github.com/NightDragon1/rconductor/internal/web"
	builtinprofiles "github.com/NightDragon1/rconductor/profiles"
)

// PanelOptions are optional hooks for RunPanel.
type PanelOptions struct {
	// UI serves the embedded web interface.
	UI http.Handler
	// Ready is called with the bound web address once the panel serves.
	Ready func(httpAddr net.Addr)
	// AgentReady is called with the bound agent port before Ready.
	AgentReady func(agentAddr net.Addr)
	// Console receives the one-time initial password of the emergency
	// admin (AUTH-007), apart from the event log; nil means os.Stderr.
	Console io.Writer
	// HashParams override the Argon2id cost, for tests only.
	HashParams *password.Params
}

// RunPanel starts the panel and blocks until ctx ends: migrations (with a
// backup first), master key, backup schedule, web port. Errors are logged
// and returned.
func RunPanel(ctx context.Context, cfg config.Panel, out io.Writer, opts PanelOptions) error {
	logs := logging.New(out, "panel", cfg.Log)
	root := logs.Root()
	root.Info("starting", logging.Event(logging.AppStart), slog.String(logging.KeyVersion, buildinfo.Version))

	err := runPanel(ctx, cfg, logs, opts)
	if err != nil {
		root.Error("panel stopped with error", slog.Any("err", err))
	}
	return err
}

func runPanel(ctx context.Context, cfg config.Panel, logs *logging.Factory, opts PanelOptions) error {
	dbLog := logs.Module("db")
	st, err := store.Open(ctx, cfg.DataDir)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	backups := backup.New(st.DB, cfg.BackupDir, logs.Module("backup"))
	if _, err := st.Migrate(ctx, dbLog, store.MigrateOptions{
		BeforeMigrate: backup.BeforeMigrate(st, backups),
	}); err != nil {
		return err
	}

	masterKey, err := keys.LoadOrCreate(logs.Module("keys"), keys.Options{
		SecretsDir: cfg.SecretsDir,
		File:       cfg.MasterKeyFile,
		Forbidden:  []string{cfg.DataDir, cfg.BackupDir},
	})
	if err != nil {
		return err
	}
	box, err := secret.New(masterKey.Bytes())
	if err != nil {
		return err
	}

	if err := backup.EnsureDefaults(ctx, st); err != nil {
		return err
	}
	if err := auth.EnsureDefaults(ctx, st); err != nil {
		return err
	}
	if err := perm.EnsureDefaults(ctx, st); err != nil {
		return err
	}
	if err := users.EnsureDefaults(ctx, st); err != nil {
		return err
	}
	if err := users.EnsureAuditDefaults(ctx, st); err != nil {
		return err
	}
	if err := profiles.EnsureDefaults(ctx, st); err != nil {
		return err
	}
	// Catalog import before serving (Architecture 5.4), then on every change.
	importer := profiles.NewImporter(st, logs.Module("profiles"), builtinprofiles.FS(), filepath.Join(cfg.DataDir, "profiles"))
	if err := importer.Sync(ctx); err != nil {
		return err
	}

	params := password.Default
	if opts.HashParams != nil {
		params = *opts.HashParams
	}
	hasher, err := password.NewHasher(params, cfg.HashConcurrency, password.DefaultWait)
	if err != nil {
		return err
	}
	console := opts.Console
	if console == nil {
		console = os.Stderr
	}
	authLog := logs.Module("auth")
	if err := auth.EnsureEmergencyAdmin(ctx, st, hasher,
		auth.EmergencyOptions{Enabled: cfg.EmergencyAdmin, Reset: cfg.EmergencyAdminReset}, authLog, console); err != nil {
		return err
	}
	authSvc := auth.NewService(st, hasher, authLog, cfg.EmergencyAdmin)
	checker := perm.NewChecker(st, logs.Module("perm"))
	userSvc := users.NewService(st, checker, logs.Module("users"), authSvc.RevokeUser)
	serverSvc := servers.NewService(st, checker, box, logs.Module("servers"))
	if err := serverSvc.CheckCredentials(ctx); err != nil {
		return err
	}
	// RCON connections before serving (Architecture 5.4); they keep
	// reconnecting in the background.
	var runtime *cache.Cache
	var hub *eventbus.Hub
	conns := connmgr.New(rcon.Registry{"source-rcon": source.Adapter{}}, serverSvc, logs.Module("rcon"), connmgr.Options{
		OnStatus: func(id string, s connmgr.Status) {
			if s != connmgr.Connected && runtime != nil {
				runtime.Invalidate(id, "rcon_"+string(s))
			}
			if hub != nil {
				hub.PublishStatus(id, s, time.Now())
			}
		},
	})
	runtime = cache.New(st, conns, logs.Module("cache"))
	hub = eventbus.NewHub(checker, runtime, authSvc, logs.Module("ws"), eventbus.SettingsLimits(st))
	authSvc.OnSessionsEnded(hub.CloseSessions)
	serverSvc.SetConnections(conns)
	if err := conns.Start(ctx); err != nil {
		return err
	}
	defer conns.Wait()
	if err := commands.EnsureDefaults(ctx, st); err != nil {
		return err
	}
	cmdSvc := commands.NewService(st, checker, conns, logs.Module("cmd"))
	cmdSvc.Publish = hub.PublishConsole

	var wg sync.WaitGroup
	defer wg.Wait()
	wg.Go(func() { backup.Schedule(ctx, st, backups) })
	wg.Go(func() { cleanup.Schedule(ctx, st, logs.Module("audit")) })
	wg.Go(func() { importer.Watch(ctx) })

	// Agent port with the panel's own key pair (Design 1.1, NFR-001).
	panelKey, err := keys.LoadOrCreateIdentity(logs.Module("keys"), filepath.Join(cfg.SecretsDir, keys.PanelKeyFile))
	if err != nil {
		return err
	}
	agentSvc := agentadmin.NewService(st, logs.Module("agent"), cfg.AgentPublicAddr)
	agents := agenthub.New(panelKey, logs.Module("agent"), agenthub.Options{Registry: agentSvc})
	agentData := cache.NewAgentData(logs.Module("cache"))
	agents.OnMessage(agentData.Receive)
	agents.OnDisconnect(agentData.Disconnected)
	serverSvc.SetActions(agents, conns)
	views := agentview.New(st, agentData, agents, hub, logs.Module("agent"))
	views.SetPlayers(runtime)
	hub.SetAgentViews(views)
	agentSvc.SetHub(agents)
	wg.Go(func() { agentSvc.ClosePairing(ctx, time.Minute) })
	agentAddr := make(chan net.Addr, 1)
	agentErr := make(chan error, 1)
	wg.Go(func() { agentErr <- agents.Serve(ctx, cfg.AgentAddr, func(a net.Addr) { agentAddr <- a }) })
	var boundAgent net.Addr
	select {
	case boundAgent = <-agentAddr:
	case err := <-agentErr:
		return fmt.Errorf("agent port %s: %w", cfg.AgentAddr, err)
	}
	if opts.AgentReady != nil {
		opts.AgentReady(boundAgent)
	}

	webLog := logs.Module("web")
	proxies := web.Proxies{Trusted: cfg.TrustedProxies, PublicURL: cfg.PublicURL}
	if !proxies.HTTPSKnown() {
		webLog.Warn("RCD_PUBLIC_URL is not https: session cookies are only marked Secure " +
			"when a trusted proxy (RCD_TRUSTED_PROXIES) sends X-Forwarded-Proto: https")
	}
	handler := web.NewHandler(web.Options{Log: webLog, UI: opts.UI, Proxies: proxies, Store: st, Servers: serverSvc, Commands: cmdSvc, Cache: runtime, Hub: hub, Auth: authSvc, Perm: checker, Users: userSvc,
		Invites: users.NewInvites(userSvc, hasher), Profiles: users.NewProfiles(userSvc, hasher, authSvc.RevokeOtherSessions),
		Agents: agentSvc, Runtime: views,
	})
	return web.Serve(ctx, cfg.HTTPAddr, netlimit.Options{Max: cfg.MaxConnections, PerClient: cfg.MaxClientConnections, Exempt: proxies.Trust}, handler, webLog,
		func(addr net.Addr) {
			logs.Root().Info("ready", logging.Event(logging.AppReady),
				slog.String(logging.KeyVersion, buildinfo.Version), slog.String("http_addr", addr.String()),
				slog.String("agent_addr", boundAgent.String()), slog.String("panel_fingerprint", panelKey.Fingerprint()))
			if opts.Ready != nil {
				opts.Ready(addr)
			}
		})
}
