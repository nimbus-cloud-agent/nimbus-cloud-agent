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
