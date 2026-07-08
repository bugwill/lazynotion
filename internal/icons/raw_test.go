package icons

import "testing"

func TestFromRaw(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want Icon
	}{
		{"null", `null`, Icon{}},
		{"emoji", `{"type":"emoji","emoji":"🚀"}`, Icon{Emoji: "🚀"}},
		{"external builtin", `{"type":"external","external":{"url":"https://www.notion.so/icons/checklist_green.svg"}}`,
			Icon{Name: "checklist", Color: "green"}},
		{"external custom", `{"type":"external","external":{"url":"https://images.crunchbase.com/x.svg"}}`,
			Icon{Custom: true}},
		{"file upload", `{"type":"file","file":{"url":"https://s3.amazonaws.com/x/logo.png","expiry_time":"2026-01-01T00:00:00Z"}}`,
			Icon{Custom: true}},
		{"custom emoji", `{"type":"custom_emoji","custom_emoji":{"id":"1","name":"blob","url":"https://s3.amazonaws.com/e.png"}}`,
			Icon{Custom: true}},
		// plausible shapes of the new "icon" type — the walker finds
		// name/color or a library URL wherever they nest
		{"icon type nested name+color", `{"type":"icon","icon":{"name":"clipboard","color":"gray"}}`,
			Icon{Name: "clipboard", Color: "gray"}},
		{"icon type flat", `{"type":"icon","name":"arrow_up","color":"blue"}`,
			Icon{Name: "arrow-up", Color: "blue"}},
		{"icon type with library url", `{"type":"icon","icon":{"url":"https://www.notion.so/icons/light-bulb_yellow.svg?mode=dark"}}`,
			Icon{Name: "light-bulb", Color: "yellow"}},
		{"icon type opaque", `{"type":"icon","icon":{"id":"abc123"}}`,
			Icon{Custom: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := FromRaw([]byte(tc.raw))
			if got != tc.want {
				t.Errorf("FromRaw(%s) = %+v, want %+v", tc.raw, got, tc.want)
			}
		})
	}
}
