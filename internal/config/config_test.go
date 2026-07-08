package config

import (
	"os"
	"path/filepath"
	"testing"
)

func loadWith(t *testing.T, contents, envToken string) (Config, error) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("NOTION_TOKEN", envToken)
	if contents != "" {
		if err := os.MkdirAll(filepath.Join(dir, "lazynotion"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "lazynotion", "config.toml"), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return Load()
}

func TestLoadSingleToken(t *testing.T) {
	cfg, err := loadWith(t, `token = "ntn_one"`, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Workspaces) != 1 || cfg.Workspaces[0].Name != "default" || cfg.Workspaces[0].Token != "ntn_one" {
		t.Errorf("got %+v", cfg.Workspaces)
	}
}

func TestLoadMultiWorkspace(t *testing.T) {
	cfg, err := loadWith(t, `
default = "work"

[workspaces.personal]
token = "ntn_p"

[workspaces.work]
token = "ntn_w"
`, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Workspaces) != 2 {
		t.Fatalf("got %d workspaces", len(cfg.Workspaces))
	}
	if cfg.Workspaces[0].Name != "personal" || cfg.Workspaces[1].Name != "work" {
		t.Errorf("order = %+v", cfg.Workspaces)
	}
	if cfg.Workspaces[cfg.DefaultIndex].Name != "work" {
		t.Errorf("default = %q, want work", cfg.Workspaces[cfg.DefaultIndex].Name)
	}
}

func TestLoadEnvTokenWins(t *testing.T) {
	cfg, err := loadWith(t, `
[workspaces.personal]
token = "ntn_p"
`, "ntn_env")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Workspaces[cfg.DefaultIndex].Name != "env" {
		t.Errorf("default = %q, want env", cfg.Workspaces[cfg.DefaultIndex].Name)
	}
	if len(cfg.Workspaces) != 2 {
		t.Errorf("env workspace should coexist with configured ones: %+v", cfg.Workspaces)
	}
}

func TestLoadUnknownDefault(t *testing.T) {
	if _, err := loadWith(t, `
default = "nope"

[workspaces.personal]
token = "ntn_p"
`, ""); err == nil {
		t.Error("unknown default workspace should error")
	}
}

func TestLoadNoTokens(t *testing.T) {
	if _, err := loadWith(t, "", ""); err != ErrNoToken {
		t.Errorf("err = %v, want ErrNoToken", err)
	}
}

func TestRootPagesOnly(t *testing.T) {
	cfg, err := loadWith(t, `
root_pages_only = true

[workspaces.personal]
token = "ntn_p"

[workspaces.work]
token = "ntn_w"
root_pages_only = false
`, "")
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Workspace{}
	for _, w := range cfg.Workspaces {
		byName[w.Name] = w
	}
	if !byName["personal"].RootOnly {
		t.Error("personal should inherit the global root_pages_only")
	}
	if byName["work"].RootOnly {
		t.Error("work should override root_pages_only to false")
	}
}
