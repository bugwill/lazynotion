package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/justinm35/lazynotion/internal/config"
	"github.com/justinm35/lazynotion/internal/notion"
)

const integrationsURL = "https://www.notion.so/my-integrations"

// runAuth walks through connecting a workspace: create an integration in the
// browser, paste its token here, validate it live, save it to the config.
func runAuth() error {
	fmt.Println("Connecting a Notion workspace.")
	fmt.Println()
	if openBrowser(integrationsURL) {
		fmt.Printf("  Opening %s in your browser...\n", integrationsURL)
	} else {
		fmt.Printf("  Open %s\n", integrationsURL)
	}
	fmt.Println()
	fmt.Println("  1. Click \"New integration\"")
	fmt.Println("  2. Name it (e.g. \"lazynotion\") and pick your workspace")
	fmt.Println("  3. Under Capabilities, enable Read, Update and Insert content")
	fmt.Println("  4. Copy the \"Internal Integration Secret\"")
	fmt.Println()

	token, err := readToken()
	if err != nil {
		return err
	}
	if token == "" {
		return fmt.Errorf("no token entered")
	}

	fmt.Print("Validating... ")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client := notion.NewClient(token)
	bot, err := client.WhoAmI(ctx)
	if err != nil {
		fmt.Println("✗")
		if strings.Contains(err.Error(), "401") {
			return fmt.Errorf("Notion rejected the token (401) — did the paste get cut off?")
		}
		return err
	}
	fmt.Printf("✓ connected as %q (%s)\n", bot.Name, bot.WorkspaceName)

	pages, err := client.Search(ctx, "", false)
	if err != nil {
		pages = nil // page count is best-effort; saving still makes sense
	}

	existing := existingWorkspaceNames()
	name, err := chooseName(bot.WorkspaceName, existing)
	if err != nil {
		return err
	}

	path, err := config.SetWorkspace(name, token)
	if err != nil {
		return err
	}
	fmt.Printf("Saved workspace %q to %s\n", name, path)
	fmt.Println()

	if len(pages) == 0 {
		fmt.Println("⚠ The token works, but no pages are shared with the integration")
		fmt.Println("  yet, so lazynotion would show an empty list. In Notion, open a")
		fmt.Println("  page → ••• → Connections → your integration. Sharing a top-level")
		fmt.Println("  page includes everything nested inside it.")
	} else {
		fmt.Printf("%d pages visible. Run lazynotion to start browsing.\n", len(pages))
	}
	return nil
}

// readToken reads the token with echo off so it stays out of scrollback;
// piped stdin falls back to a plain line read.
func readToken() (string, error) {
	fmt.Print("Paste token: ")
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		raw, err := term.ReadPassword(fd)
		fmt.Println()
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(raw)), nil
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func chooseName(workspaceName string, existing map[string]bool) (string, error) {
	suggested := slugify(workspaceName)
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Printf("Workspace name [%s]: ", suggested)
		line, err := reader.ReadString('\n')
		if err != nil && strings.TrimSpace(line) == "" {
			return suggested, nil // EOF: accept the default
		}
		name := strings.TrimSpace(line)
		if name == "" {
			name = suggested
		}
		if !config.ValidWorkspaceName(name) {
			fmt.Println("  Use letters, digits, - and _ only.")
			continue
		}
		if existing[name] {
			fmt.Printf("  Workspace %q already exists. Overwrite its token? [y/N] ", name)
			answer, _ := reader.ReadString('\n')
			if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
				continue
			}
		}
		return name, nil
	}
}

func existingWorkspaceNames() map[string]bool {
	names := map[string]bool{}
	cfg, err := config.Load()
	if err != nil {
		return names // no config yet (or unreadable): nothing to collide with
	}
	for _, w := range cfg.Workspaces {
		names[w.Name] = true
	}
	return names
}

var slugStripRe = regexp.MustCompile(`[^a-z0-9_-]+`)

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "-")
	s = slugStripRe.ReplaceAllString(s, "")
	s = strings.Trim(s, "-")
	if s == "" {
		return "default"
	}
	return s
}

func openBrowser(url string) bool {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		return false
	}
	return cmd.Start() == nil
}
