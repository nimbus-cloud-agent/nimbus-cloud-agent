package main

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/config"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/executor"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/logging"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/sandbox"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/server"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/store"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/web"
)

func main() {
	slog.SetDefault(slog.New(logging.NewHandler(os.Stdout, slog.LevelInfo,
		os.Getenv("NIMBUS_E2B_API_KEY"), os.Getenv("NIMBUS_DEEPSEEK_API_KEY"),
		os.Getenv("NIMBUS_BASIC_PASS"), os.Getenv("NIMBUS_POSTGRES"))))
	cmd := config.Command(func(c config.Config) error {
		if err := logging.Configure(c.LogLevel, c.E2BKey, c.DeepSeekKey, c.BasicPass, c.Postgres); err != nil {
			return err
		}
		if err := server.ValidateConfig(c); err != nil {
			return err
		}
		sandboxes, e := sandbox.New(c.E2BKey)
		if e != nil {
			return e
		}
		lifetime, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		slog.Info("Nimbus starting", "log_level", c.LogLevel, "deploy_handoff", c.DeployHandoff)
		runtime := executor.New(c.RuntimeCommand)
		var db *store.Store
		defer func() {
			// Stop the child and all callbacks before releasing database ownership.
			runtime.Close()
			if db != nil {
				_ = db.Close()
			}
		}()
		checkCtx, checkCancel := context.WithTimeout(lifetime, 20*time.Second)
		defer checkCancel()
		if e := runtime.Check(checkCtx); e != nil {
			return e
		}
		if c.DeployHandoff {
			startup, cancel := context.WithTimeout(lifetime, 3*time.Minute)
			db, e = store.OpenWithHandoff(startup, c.Postgres)
			cancel()
		} else {
			db, e = store.Open(c.Postgres)
		}
		if e != nil {
			return e
		}
		frontend, _ := fs.Sub(web.Dist, "dist")
		for {
			srv, e := server.New(c, db, frontend, runtime, sandboxes)
			if e != nil {
				return e
			}
			serverErr := make(chan error, 1)
			go func() { serverErr <- srv.HTTP.ListenAndServe() }()
			slog.Info("Nimbus ready", "addr", c.Addr)
			var reason error
			handoff := false
			select {
			case reason = <-serverErr:
				if errors.Is(reason, http.ErrServerClosed) {
					reason = nil
				}
			case <-lifetime.Done():
				slog.Info("Nimbus shutting down", "reason", "termination signal")
			case reason = <-db.OwnershipDone():
				handoff = reason == nil
				if handoff {
					slog.Info("database ownership handoff requested; draining tasks and requests")
				} else {
					slog.Error("database ownership lost; stopping Nimbus", "error", reason)
				}
			}
			if handoff {
				drainCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				reason = srv.Drain(drainCtx)
				cancel()
				if reason == nil {
					_ = db.Close()
					db = nil
					slog.Info("database ownership released; waiting for Render to retire this instance")
					// Stay alive so Render does not restart the retiring instance.
					// Recover locally if the successor fails before becoming healthy.
					db, reason = awaitSuccessor(lifetime, c.Postgres, serverErr)
					if db != nil {
						_ = srv.HTTP.Close()
						slog.Warn("successor released ownership; restoring previous instance")
						continue
					}
				}
			}
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			shutdownErr := srv.Shutdown(shutdownCtx)
			cancel()
			return errors.Join(reason, shutdownErr)
		}
	})
	if e := cmd.Execute(); e != nil {
		slog.Error("command failed", "error", e)
		os.Exit(1)
	}
}

func awaitSuccessor(ctx context.Context, dsn string, serverErr <-chan error) (*store.Store, error) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, nil
		case err := <-serverErr:
			return nil, err
		case <-ticker.C:
			probe, cancel := context.WithTimeout(ctx, 5*time.Second)
			db, err := store.TryOpenWithHandoff(probe, dsn)
			cancel()
			if err == nil {
				return db, nil
			}
			if ctx.Err() == nil && !errors.Is(err, store.ErrDatabaseOwned) {
				slog.Warn("could not check successor database ownership", "error", err)
			}
		}
	}
}
