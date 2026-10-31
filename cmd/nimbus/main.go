package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/config"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/executor"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/server"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/internal/store"
	"github.com/nimbus-cloud-agent/nimbus-cloud-agent/web"
)

func main() {
	cmd := config.Command(func(c config.Config) error {
		runtime := executor.New(c.RuntimeCommand)
		defer runtime.Close()
		checkCtx, checkCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer checkCancel()
		if e := runtime.Check(checkCtx); e != nil {
			return e
		}
		db, e := store.Open(c.Postgres)
		if e != nil {
			return e
		}
		defer db.Close()
		frontend, _ := fs.Sub(web.Dist, "dist")
		srv, e := server.New(c, db, frontend, runtime)
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
