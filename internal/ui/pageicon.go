package ui

import (
	"strings"
	"unicode"

	"github.com/justinm35/lazynotion/internal/icons"
)

// parseIconInput understands two icon spellings: a literal emoji ("🎯"),
// or a Notion built-in icon as "name color" ("target red", color optional,
// defaulting to gray). Returns the API payload and the local icon.
func parseIconInput(value string) (map[string]any, icons.Icon, bool) {
	fields := strings.Fields(value)
	switch len(fields) {
	case 1:
		if !asciiWord(fields[0]) {
			return map[string]any{"type": "emoji", "emoji": fields[0]},
				icons.Icon{Emoji: fields[0]}, true
		}
		name := strings.ToLower(fields[0])
		return builtinIconPayload(name, "gray"), icons.Icon{Name: name, Color: "gray"}, true
	case 2:
		if asciiWord(fields[0]) && asciiWord(fields[1]) {
			name := strings.ToLower(fields[0])
			color := strings.ToLower(fields[1])
			return builtinIconPayload(name, color), icons.Icon{Name: name, Color: color}, true
		}
	}
	return nil, icons.Icon{}, false
}

func builtinIconPayload(name, color string) map[string]any {
	url := "https://www.notion.so/icons/" + name + "_" + color + ".svg"
	return map[string]any{
		"type":     "external",
		"external": map[string]any{"url": url},
	}
}

func asciiWord(s string) bool {
	for _, r := range s {
		if r > unicode.MaxASCII {
			return false
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_' {
			return false
		}
	}
	return len(s) > 0
}
