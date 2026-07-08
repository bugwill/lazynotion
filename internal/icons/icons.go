package icons

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/jomei/notionapi"
)

// Icon is a page or callout icon in one of Notion's three shapes: an emoji,
// a built-in library icon (named + colored), or an uploaded custom image.
type Icon struct {
	Emoji  string
	Name   string
	Color  string
	Custom bool
}

func (i Icon) IsZero() bool {
	return i.Emoji == "" && i.Name == "" && !i.Custom
}

// Notion's built-in icon library serves URLs like
// https://www.notion.so/icons/checklist_gray.svg — names may contain
// dashes or underscores; the color always follows the last underscore.
var builtinRe = regexp.MustCompile(`/icons/([a-z0-9_-]+)_([a-z]+)\.(?:svg|png)`)

func FromNotion(icon *notionapi.Icon) Icon {
	if icon == nil {
		return Icon{}
	}
	if icon.Emoji != nil && *icon.Emoji != "" {
		return Icon{Emoji: string(*icon.Emoji)}
	}
	var url string
	if icon.External != nil {
		url = icon.External.URL
	} else if icon.File != nil {
		url = icon.File.URL
	}
	if m := builtinRe.FindStringSubmatch(url); m != nil {
		return Icon{Name: strings.ReplaceAll(m[1], "_", "-"), Color: m[2]}
	}
	// Anything else that is present but unparseable — uploaded images,
	// custom emoji (which the client library cannot decode), unknown
	// shapes — gets the fallback marker rather than disappearing.
	if url != "" || icon.Type != "" {
		return Icon{Custom: true}
	}
	return Icon{}
}

// Glyph returns the uncolored terminal representation — safe to embed in
// markdown that passes through glamour.
func (i Icon) Glyph() string {
	switch {
	case i.Emoji != "":
		return i.Emoji
	case i.Name != "":
		return glyphFor(i.Name)
	case i.Custom:
		return "◆"
	}
	return ""
}

// Render returns the glyph with its Notion color applied; emoji pass
// through unchanged since they carry their own color.
func (i Icon) Render() string {
	if i.Emoji != "" {
		return i.Emoji
	}
	glyph := i.Glyph()
	if glyph == "" {
		return ""
	}
	return lipgloss.NewStyle().Foreground(TermColor(i.Color)).Render(glyph)
}

func glyphFor(name string) string {
	for {
		if g, ok := glyphs[name]; ok {
			return g
		}
		idx := strings.LastIndex(name, "-")
		if idx < 0 {
			return "◆"
		}
		name = name[:idx]
	}
}

// TermColor maps Notion's icon color names onto ANSI-256 colors.
func TermColor(color string) lipgloss.Color {
	switch color {
	case "lightgray":
		return lipgloss.Color("250")
	case "brown":
		return lipgloss.Color("137")
	case "yellow":
		return lipgloss.Color("220")
	case "orange":
		return lipgloss.Color("208")
	case "green":
		return lipgloss.Color("77")
	case "blue":
		return lipgloss.Color("75")
	case "purple":
		return lipgloss.Color("135")
	case "pink":
		return lipgloss.Color("212")
	case "red":
		return lipgloss.Color("203")
	default: // gray and anything unknown
		return lipgloss.Color("245")
	}
}

// glyphs maps Notion built-in icon names to single-cell terminal glyphs,
// chosen from widely-supported unicode ranges (dingbats, geometric shapes,
// arrows). Related names share a glyph; color differentiates them.
var glyphs = map[string]string{
	// documents & writing
	"document": "▤", "note": "▤", "page": "▤", "journal": "▤",
	"newspaper": "▤", "clipboard": "▤", "receipt": "▤",
	"list": "☰", "checklist": "✓", "checkmark": "✓", "to-do": "✓",
	"pencil": "✎", "pen": "✎", "compose": "✎", "edit": "✎", "highlighter": "✎",
	// books & learning
	"book": "▥", "library": "▥", "notebook": "▥", "bookshelf": "▥",
	"graduate": "▥", "education": "▥", "school": "▥",
	// data & tech
	"database": "▦", "table": "▦", "layers": "▦", "stack": "▦",
	"graph": "▦", "chart": "▦", "kanban": "▦", "growth": "↗", "trending": "↗",
	"code": "⌗", "terminal": "⌗", "computer": "⌗", "keyboard": "⌗", "calculator": "⌗",
	"gear": "⚙", "settings": "⚙", "tools": "⚙", "wrench": "⚙", "hammer": "⚙", "screwdriver": "⚙",
	"wifi": "≋", "cloud": "≋", "signal": "≋",
	// communication
	"mail": "✉", "inbox": "✉", "envelope": "✉", "send": "✈",
	"paper-airplane": "✈", "airplane": "✈",
	"chat": "❑", "thought": "❑", "comment": "❑", "discussion": "❑",
	"megaphone": "◀", "announcement": "◀", "microphone": "♪",
	"phone": "☏", "call": "☏",
	// people
	"people": "☺", "person": "☺", "user": "☺", "team": "☺",
	"meeting": "☺", "baby": "☺", "face": "☺",
	// time & planning
	"calendar": "◫", "date": "◫", "schedule": "◫",
	"clock": "◷", "alarm": "◷", "timer": "◷", "hourglass": "◷", "history": "◷",
	// places
	"map-pin": "⌖", "pin": "⌖", "location": "⌖", "compass": "⌖", "map": "⌖",
	"globe": "◍", "world": "◍", "earth": "◍",
	"home": "⌂", "house": "⌂", "city": "⌂", "building": "⌂", "bank": "⌂", "office": "⌂",
	"mountain": "▲", "tent": "▲",
	// finance
	"wallet": "¤", "credit-card": "¤", "piggy-bank": "¤", "coin": "¤",
	"cash": "¤", "money": "¤", "currency": "¤", "payment": "¤",
	// nature & food
	"leaf": "❧", "tree": "❧", "plant": "❧", "flower": "❧", "sprout": "❧",
	"coffee": "♨", "drink": "♨", "fork-knife": "♨", "food": "♨", "cake": "♨", "apple": "♨",
	"sun": "☼", "moon": "☾", "snowflake": "❄", "umbrella": "☂", "drop": "❉",
	// symbols
	"star": "★", "sparkles": "✦", "sparkle": "✦", "new": "✦",
	"heart": "♥", "like": "♥", "music": "♪", "headphones": "♪",
	"flag": "⚑", "bookmark": "⚑", "milestone": "⚑",
	"target": "◎", "priority": "◎", "goal": "◎", "focus": "◎",
	"trophy": "♛", "crown": "♛", "medal": "♛", "award": "♛",
	"rocket": "↗", "launch": "↗",
	"arrow-up": "↑", "arrow-right": "→", "arrow-down": "↓", "arrow-left": "←",
	"refresh": "↻", "sync": "↻", "repeat": "↻", "loop": "↻",
	"light-bulb": "◉", "idea": "◉", "camera": "◉", "photo": "◉", "eye": "◉",
	"movie": "▶", "video": "▶", "playback": "▶", "play": "▶",
	"folder": "▨", "archive": "▨", "box": "▨", "package": "▨", "drawer": "▨",
	"link": "∞", "paperclip": "∞", "attachment": "∞",
	"tag": "◇", "token": "◇", "label": "◇",
	"bell": "◔", "notification": "◔", "reminder": "◔",
	"lock": "◈", "shield": "◈", "security": "◈", "key": "◈", "private": "◈",
	"trash": "✕", "delete": "✕", "remove": "✕", "close": "✕",
	"warning": "△", "exclamation-mark": "△", "alert": "△", "error": "△",
	"info": "ℹ", "question-mark": "?", "help": "?",
	"science": "∴", "atom": "∴", "dna": "∴", "microscope": "∴", "thermometer": "∴",
	"car": "➤", "truck": "➤", "train": "➤", "bike": "➤", "scooter": "➤",
	"gift": "❒", "shopping": "⊞", "cart": "⊞", "bag": "⊞",
	"puzzle": "⊡", "game": "⊡", "dice": "⊡",
	"bug": "✱", "fire": "▲", "bolt": "↯", "lightning": "↯", "activity": "↯", "pulse": "↯",
	"palette": "❖", "paint": "❖", "brush": "❖", "design": "❖", "art": "❖",
	"briefcase": "▣", "work": "▣", "suitcase": "▣",
	"sport": "●", "basketball": "●", "soccer": "●", "barbell": "●", "gym": "●",
}
