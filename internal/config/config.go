package config

import (
	"fmt"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"os"
	"strings"
)

type Config struct{ Addr, Postgres, HarnessPath, RuntimeDir, WorkDir, DeepSeekKey, E2BKey, E2BTemplate string }

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
	f.String("harness-path", "node_modules/.bin/dsh", "Installed DeepSeek Harness 0.2.1-rc.5")
	f.String("runtime-dir", "runtime", "Nimbus runtime directory")
	f.String("work-dir", ".local/turns", "Per-turn working files")
	f.String("deepseek-api-key", "", "DeepSeek API key")
	f.String("e2b-api-key", "", "E2B API key (required)")
	f.String("e2b-template", "base", "E2B sandbox template")
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
		c := Config{Addr: v.GetString("addr"), Postgres: v.GetString("postgres"), HarnessPath: v.GetString("harness-path"), RuntimeDir: v.GetString("runtime-dir"), WorkDir: v.GetString("work-dir"), DeepSeekKey: v.GetString("deepseek-api-key"), E2BKey: v.GetString("e2b-api-key"), E2BTemplate: v.GetString("e2b-template")}
		if c.Postgres == "" {
			return fmt.Errorf("--postgres or NIMBUS_POSTGRES is required")
		}
		if c.E2BKey == "" {
			return fmt.Errorf("--e2b-api-key or NIMBUS_E2B_API_KEY is required")
		}
		if _, e := os.Stat(c.HarnessPath); e != nil {
			return fmt.Errorf("install DeepSeek Harness 0.2.1-rc.5 and set --harness-path: %w", e)
		}
		return run(c)
	}
	root.AddCommand(&cobra.Command{Use: "serve", RunE: root.RunE}, &cobra.Command{Use: "version", Run: func(cmd *cobra.Command, args []string) {
		fmt.Fprintln(cmd.OutOrStdout(), "Nimbus 0.1.0 / DSH 0.2.1-rc.5")
	}})
	return root
}
