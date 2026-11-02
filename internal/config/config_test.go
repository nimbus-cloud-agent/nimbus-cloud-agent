package config

import (
	"os"
	"testing"
)

func TestDeploymentLoggingFlagsOverrideEnvironment(t *testing.T) {
	t.Setenv("NIMBUS_POSTGRES", "postgres://test.invalid/test")
	t.Setenv("NIMBUS_E2B_API_KEY", "test-key")
	t.Setenv("NIMBUS_LOG_LEVEL", "debug")
	t.Setenv("NIMBUS_DEPLOY_HANDOFF", "true")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, override := range []bool{false, true} {
		var got Config
		cmd := Command(func(c Config) error { got = c; return nil })
		args := []string{"serve", "--runtime-command", executable}
		if override {
			args = append(args, "--log-level", "error", "--deploy-handoff=false")
		}
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		wantLevel := "debug"
		if override {
			wantLevel = "error"
		}
		if got.LogLevel != wantLevel || got.DeployHandoff == override {
			t.Fatalf("override=%v: level=%s, handoff=%v", override, got.LogLevel, got.DeployHandoff)
		}
	}
}

func TestDatabaseEnvironmentPrefersDirectNeonConnection(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, nimbus, direct, pooled, flag, want string
	}{
		{"neon direct", "", "postgres://direct/test", "postgres://pooler/test", "", "postgres://direct/test"},
		{"generic URL", "", "", "postgres://generic/test", "", "postgres://generic/test"},
		{"explicit Nimbus", "postgres://nimbus/test", "postgres://direct/test", "postgres://pooler/test", "", "postgres://nimbus/test"},
		{"explicit flag", "postgres://nimbus/test", "postgres://direct/test", "postgres://pooler/test", "postgres://flag/test", "postgres://flag/test"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("NIMBUS_POSTGRES", tc.nimbus)
			t.Setenv("DATABASE_URL_UNPOOLED", tc.direct)
			t.Setenv("DATABASE_URL", tc.pooled)
			t.Setenv("NIMBUS_E2B_API_KEY", "test-key")
			var got Config
			cmd := Command(func(c Config) error { got = c; return nil })
			args := []string{"serve", "--runtime-command", executable}
			if tc.flag != "" {
				args = append(args, "--postgres", tc.flag)
			}
			cmd.SetArgs(args)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			if got.Postgres != tc.want {
				t.Fatalf("got %q, want %q", got.Postgres, tc.want)
			}
		})
	}
}
