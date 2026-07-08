package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetWorkspaceInTOMLAppendsToEmpty(t *testing.T) {
	got := setWorkspaceInTOML("", "personal", "ntn_abc")
	want := "[workspaces.personal]\ntoken = \"ntn_abc\"\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSetWorkspaceInTOMLPreservesExistingContent(t *testing.T) {
	in := "# my config\naccent = \"205\"\n\n[workspaces.work]\ntoken = \"ntn_work\"\n"
	got := setWorkspaceInTOML(in, "personal", "ntn_abc")
	for _, want := range []string{"# my config", "accent = \"205\"", "token = \"ntn_work\"", "[workspaces.personal]\ntoken = \"ntn_abc\""} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

func TestSetWorkspaceInTOMLReplacesToken(t *testing.T) {
	in := "[workspaces.personal]\ntoken = \"ntn_old\"\nroot_pages_only = true\n\n[workspaces.work]\ntoken = \"ntn_work\"\n"
	got := setWorkspaceInTOML(in, "personal", "ntn_new")
	if strings.Contains(got, "ntn_old") {
		t.Errorf("old token survived:\n%s", got)
	}
	for _, want := range []string{"token = \"ntn_new\"", "root_pages_only = true", "token = \"ntn_work\""} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
	if strings.Count(got, "[workspaces.personal]") != 1 {
		t.Errorf("duplicate table:\n%s", got)
	}
}

func TestSetWorkspaceInTOMLTableWithoutToken(t *testing.T) {
	in := "[workspaces.personal]\nroot_pages_only = true\n"
	got := setWorkspaceInTOML(in, "personal", "ntn_abc")
	idxHeader := strings.Index(got, "[workspaces.personal]")
	idxToken := strings.Index(got, "token = \"ntn_abc\"")
	if idxToken < idxHeader {
		t.Errorf("token not inside table:\n%s", got)
	}
	if !strings.Contains(got, "root_pages_only = true") {
		t.Errorf("table body lost:\n%s", got)
	}
}

func TestSetWorkspaceWritesFileWithTightPerms(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	path, err := SetWorkspace("personal", "ntn_abc")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "lazynotion", "config.toml"); path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("perm = %o, want 600", perm)
	}

	// Round-trip through Load.
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Workspaces) != 1 || cfg.Workspaces[0].Name != "personal" || cfg.Workspaces[0].Token != "ntn_abc" {
		t.Errorf("Load = %+v", cfg.Workspaces)
	}
}

func TestSetWorkspaceRejectsBadName(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if _, err := SetWorkspace("bad name]", "ntn_abc"); err == nil {
		t.Error("expected error for invalid name")
	}
}
