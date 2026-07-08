package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var workspaceNameRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// ValidWorkspaceName reports whether name can be used as a bare TOML key.
func ValidWorkspaceName(name string) bool {
	return workspaceNameRe.MatchString(name)
}

// SetWorkspace writes or replaces a [workspaces.<name>] entry in the config
// file. Edits are line-level so comments, theming keys and other workspaces
// survive untouched. The file is created 0600 (it holds tokens).
func SetWorkspace(name, token string) (string, error) {
	if !ValidWorkspaceName(name) {
		return "", fmt.Errorf("workspace name %q: use letters, digits, - and _ only", name)
	}
	path, err := Path()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}

	content, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}

	updated := setWorkspaceInTOML(string(content), name, token)
	if err := os.WriteFile(path, []byte(updated), 0o600); err != nil {
		return "", err
	}
	// Tighten files created before auth existed.
	if err := os.Chmod(path, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func setWorkspaceInTOML(content, name, token string) string {
	header := "[workspaces." + name + "]"
	tokenLine := fmt.Sprintf("token = %q", token)
	lines := strings.Split(content, "\n")

	for i, line := range lines {
		if strings.TrimSpace(line) != header {
			continue
		}
		// Replace the token inside this table; it ends at the next header.
		for j := i + 1; j < len(lines); j++ {
			trimmed := strings.TrimSpace(lines[j])
			if strings.HasPrefix(trimmed, "[") {
				break
			}
			if strings.HasPrefix(trimmed, "token") {
				lines[j] = tokenLine
				return strings.Join(lines, "\n")
			}
		}
		// Table exists but has no token line yet.
		return strings.Join(append(lines[:i+1], append([]string{tokenLine}, lines[i+1:]...)...), "\n")
	}

	out := strings.TrimRight(content, "\n")
	if out != "" {
		out += "\n\n"
	}
	return out + header + "\n" + tokenLine + "\n"
}
