package icons

import (
	"encoding/json"
	"strings"
)

// FromRaw parses a page's raw icon JSON without assuming a schema: Notion
// has shipped at least four icon payload shapes (emoji, external, file,
// custom_emoji) plus a newer "icon" type the client library cannot decode.
// The walker finds emoji, icon-library names/colors and URLs wherever they
// nest.
func FromRaw(raw json.RawMessage) Icon {
	if len(raw) == 0 || string(raw) == "null" {
		return Icon{}
	}
	var data any
	if err := json.Unmarshal(raw, &data); err != nil {
		return Icon{Custom: true}
	}

	var (
		emoji, name, color, typ string
		sawValue                bool
		builtin                 Icon
	)
	var walk func(v any)
	walk = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			for k, child := range t {
				s, isString := child.(string)
				if !isString {
					walk(child)
					continue
				}
				sawValue = true
				switch k {
				case "emoji":
					if emoji == "" {
						emoji = s
					}
				case "type":
					if typ == "" {
						typ = s
					}
				case "name":
					if name == "" {
						name = s
					}
				case "color":
					if color == "" {
						color = s
					}
				default:
					if m := builtinRe.FindStringSubmatch(s); m != nil && builtin.Name == "" {
						builtin = Icon{Name: strings.ReplaceAll(m[1], "_", "-"), Color: m[2]}
					}
				}
			}
		case []any:
			for _, child := range t {
				walk(child)
			}
		}
	}
	walk(data)

	switch {
	case typ == "custom_emoji":
		// custom emoji have a "name" too, but it's a label, not a library icon
		return Icon{Custom: true}
	case emoji != "":
		return Icon{Emoji: emoji}
	case builtin.Name != "":
		return builtin
	case name != "":
		return Icon{Name: strings.ReplaceAll(name, "_", "-"), Color: color}
	case sawValue:
		return Icon{Custom: true}
	}
	return Icon{}
}
