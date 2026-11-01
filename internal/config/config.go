package config

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

type Config struct {
	Addr, Postgres, RuntimeCommand, WorkDir, DeepSeekKey, E2BKey, E2BTemplate string
	// PublicHost, when set, is the single public hostname allowed to reach
	// the control plane (e.g. nimbus-dev.up.railway.app). It opts the server
	// out of the default loopback-only binding and Host guard. Empty keeps
	// the historical local-only behaviour.
	PublicHost string
	// BasicUser and BasicPass, when both set, gate every route except
	// /api/health behind HTTP Basic Auth. This is the dev-deployment auth
	// gate; production deployments should use a stronger identity layer.
	BasicUser, BasicPass string
}

func Command(run func(Config) error) *cobra.Command {
	v := viper.New()
	v.SetEnvPrefix("NIMBUS")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	v.AutomaticEnv()
	root := &cobra.Command{Use: "nimbus", Short: "Nimbus — DeepSeek agents in E2B cloud sandboxes", SilenceUsage: true}
	f := root.PersistentFlags()
	f.String("config", "", "Configuration file")
	f.String("addr", "127.0.0.1:8080", "Local HTTP listen address")
	f.String("postgres", "", "PostgreSQL DSN (required)")
	f.String("runtime-command", "nimbus-agent-runtime", "Installed nimbus-agent-runtime executable")
	f.String("work-dir", ".local/turns", "Per-turn working files")
	f.String("deepseek-api-key", "", "DeepSeek API key")
	f.String("e2b-api-key", "", "E2B API key (required)")
	f.String("e2b-template", "base", "E2B sandbox template")
	f.String("public-host", "", "Public hostname allowed to reach the server (empty = loopback only)")
	f.String("basic-user", "", "Basic Auth username (both user and pass must be set to enable)")
	f.String("basic-pass", "", "Basic Auth password")
	_ = v.BindPFlags(f)
	root.RunE = func(cmd *cobra.Command, args []string) error {
		if p := v.GetString("config"); p != "" {
			v.SetConfigFile(p)
			if e := v.ReadInConfig(); e != nil {
				return e
			}
		} else {
			v.SetConfigName("config")
			v.AddConfigPath(".")
			_ = v.ReadInConfig()
		}
		c := Config{Addr: v.GetString("addr"), Postgres: v.GetString("postgres"), RuntimeCommand: v.GetString("runtime-command"), WorkDir: v.GetString("work-dir"), DeepSeekKey: v.GetString("deepseek-api-key"), E2BKey: v.GetString("e2b-api-key"), E2BTemplate: v.GetString("e2b-template"), PublicHost: v.GetString("public-host"), BasicUser: v.GetString("basic-user"), BasicPass: v.GetString("basic-pass")}
		if c.Postgres == "" {
			return fmt.Errorf("--postgres or NIMBUS_POSTGRES is required")
		}
		if c.E2BKey == "" {
			return fmt.Errorf("--e2b-api-key or NIMBUS_E2B_API_KEY is required")
		}
		if _, e := exec.LookPath(c.RuntimeCommand); e != nil {
			return fmt.Errorf("install nimbus-agent-runtime with npm or set --runtime-command: %w", e)
		}
		return run(c)
	}
	root.AddCommand(&cobra.Command{Use: "serve", RunE: root.RunE}, &cobra.Command{Use: "version", Run: func(cmd *cobra.Command, args []string) {
		fmt.Fprintln(cmd.OutOrStdout(), "Nimbus 0.1.0 / agent-runtime protocol v3")
	}})
	return root
}
