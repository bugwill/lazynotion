package ui

import (
	"testing"
)

func TestParseIconInput(t *testing.T) {
	payload, icon, ok := parseIconInput("🎯")
	if !ok || payload["type"] != "emoji" || icon.Emoji != "🎯" {
		t.Errorf("emoji input = %v %+v %v", payload, icon, ok)
	}

	payload, icon, ok = parseIconInput("target red")
	if !ok || icon.Name != "target" || icon.Color != "red" {
		t.Fatalf("builtin input = %+v %v", icon, ok)
	}
	ext := payload["external"].(map[string]any)
	if ext["url"] != "https://www.notion.so/icons/target_red.svg" {
		t.Errorf("builtin url = %v", ext["url"])
	}

	_, icon, ok = parseIconInput("clipboard")
	if !ok || icon.Color != "gray" {
		t.Errorf("single name should default to gray: %+v %v", icon, ok)
	}

	if _, _, ok := parseIconInput("too many words here"); ok {
		t.Error("three words should not parse")
	}
}
