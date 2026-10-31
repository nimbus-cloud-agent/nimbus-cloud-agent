package main

import (
	"context"
	"fmt"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/config"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/executor"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/server"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/store"
	runtimeassets "github.com/nimbus-cloud-agent/nimbus-cloud-agent/runtime"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/web"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	cmd := config.Command(func(c config.Config) error {
		// Extract the embedded, trusted runtime when no source runtime is present.
		if _, err := os.Stat(filepath.Join(c.RuntimeDir, "turn.mjs")); os.IsNotExist(err) {
			if err = os.MkdirAll(c.RuntimeDir, 0700); err != nil {
				return err
			}
			entries, _ := runtimeassets.Files.ReadDir(".")
			for _, entry := range entries {
				data, _ := runtimeassets.Files.ReadFile(entry.Name())
				if err = os.WriteFile(filepath.Join(c.RuntimeDir, entry.Name()), data, 0600); err != nil {
					return err
				}
			}
		}
		checkCtx, checkCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer checkCancel()
		if _, e := (executor.Runtime{Dir: c.RuntimeDir}).Call(checkCtx, "check", map[string]any{"harness_path": c.HarnessPath}); e != nil {
			return e
		}
		db, e := store.Open(c.Postgres)
		if e != nil {
			return e
		}
		frontend, _ := fs.Sub(web.Dist, "dist")
		srv, e := server.New(c, db, frontend)
		if e != nil {
			return e
		}
		err := make(chan error, 1)
		go func() { err <- srv.HTTP.ListenAndServe() }()
		fmt.Printf("Nimbus ready at http://%s\n", c.Addr)
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		select {
		case e := <-err:
			return e
		case <-sig:
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			return srv.Shutdown(ctx)
		}
	})
	if e := cmd.Execute(); e != nil {
		os.Exit(1)
	}
}
