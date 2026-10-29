// Command nimbus is the single-binary entry point for Nimbus Cloud Agent.
//
// It parses startup flags, opens PostgreSQL, and serves the embedded web UI
// plus the API on one HTTP port. See README.md and ARCHITECTURE.md.
//
// This is a scaffold: the server, session, VM, and store packages are wired
// in as they land. The only startup parameter that is required today is
// --postgres.
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	addr := flag.String("addr", ":8080", "Address the HTTP server listens on.")
	postgres := flag.String("postgres", "", "PostgreSQL connection string (required).")
	deepseekAPIKey := flag.String("deepseek-api-key", "", "DeepSeek API key (optional; can also be set in the web UI).")
	harnessPath := flag.String("harness-path", "", "Path to a DeepSeek Harness (dsh) executable (optional).")
	vmImage := flag.String("vm-image", "", "VM base image (optional; can also be set in the web UI).")
	flag.Parse()

	if *postgres == "" {
		fmt.Fprintln(os.Stderr, "nimbus: --postgres is required (PostgreSQL connection string)")
		flag.Usage()
		os.Exit(2)
	}
	_ = deepseekAPIKey
	_ = harnessPath
	_ = vmImage

	fmt.Printf("nimbus: starting on %s (server implementation landing in internal/server)\n", *addr)
}
