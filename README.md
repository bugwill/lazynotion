# lazynotion

A lazygit-style TUI for browsing your Notion workspace.

## Setup

1. Create an internal integration at <https://www.notion.so/my-integrations>
   with the **Read content**, **Update content** and **Insert content**
   capabilities (the last two power to-do toggling and editing)
2. In Notion, share the pages you want to browse with the integration
   (page menu → Connections → your integration)
3. Provide the token in `~/.config/lazynotion/config.toml`:

```toml
token = "ntn_..."
```

or, with several workspaces (one integration + token per workspace —
switch with `w` in the sidebar):

```toml
default = "personal"

[workspaces.personal]
token = "ntn_..."

[workspaces.work]
token = "ntn_..."
```

`export NOTION_TOKEN=ntn_...` also works for a quick start and takes
precedence when set.

To keep the sidebar to top-level pages only (nested pages are reached by
navigating into their parents, or via `/` search, which always looks
everywhere):

```toml
root_pages_only = true   # global, or per-workspace inside [workspaces.x]
```

Note: with this on, a nested page whose parent isn't shared with the
integration can only be reached through search.

## Run

```sh
go run ./cmd/lazynotion
```

## Keys

| Key | Action |
| --- | --- |
| `tab` / `1` / `2` | switch pane focus |
| `j` / `k` | move (pages in sidebar, blocks in viewer) |
| `enter` | open page/sub-page · fold toggles · elsewhere: stamp a new empty block below |
| `/` | search workspace (sidebar) / find in page (viewer) |
| `]` / `[` | next / previous find match |
| `u` | undo last delete, edit, or toggle |
| `w` | switch workspace (sidebar) |
| `?` | help overlay |
| `space` | toggle to-do under cursor |
| `i` / `e` | edit block in place (`esc` saves, `ctrl+c` discards) |
| `n` / `N` | viewer: new block below / above cursor · sidebar (`n`): new page under the highlighted one |
| `J` / `K` | move block down / up (recreates a block — comments on it are lost) |
| `d` | delete block under cursor (goes to Notion's Trash) |
| `a` | append a paragraph after cursor |
| `esc` | back to parent page, then sidebar |
| `E` | edit whole page in `$EDITOR` |
| `r` | viewer: refresh current page · sidebar: refresh everything |
| `y` / `Y` | yank block content (markdown) · copy block/page link |
| `v` | visual select: `j`/`k` extend across blocks, `y` yanks them all |
| `I` | set the page icon: an emoji (`🎯`) or a Notion built-in (`target red`) |
| `ctrl+o` | open page in browser |
| `q` | quit |

## Editing

Quick edits (`space`, `i`, `n`, `a`) apply instantly and sync to Notion in
the background. `i` swaps the block under the cursor for an inline editor
right where it sits — type (multi-line works, `enter` adds a line), then
`esc` to save or `ctrl+c` to discard. `shift+enter` (or `alt+enter` in
terminals that can't distinguish shift+enter from enter — iTerm2 and
Terminal.app by default) saves the block and opens a new one below,
Notion-style, so you can chain blocks without leaving the keyboard flow.

Markdown works while editing: `**bold**`, `*italic*`, `` `code` ``,
`[link](url)` and `~~strike~~` become real Notion formatting on save, and
existing formatting shows up as markdown when you edit a block, so it
round-trips. New blocks (`n` and `a`) additionally understand block syntax:
`# heading`, `- bullet`, `1. numbered`, `- [ ] to-do`, `> quote`, `---`, and
fenced code create properly-typed blocks; several lines create several
blocks. Lists continue notion-style: `enter` on a `- [ ]`, `- ` or `1. `
item saves it and opens the next item with the marker pre-typed; `enter` on
an empty marker ends the list.

Pressing `n` with the cursor on an open toggle (or toggleable heading) adds
the new block *inside* its body; on a nested block, `n`/`N` create siblings
at the same nesting level. Editing a toggle shows its `**▸ title**` syntax,
which round-trips like every other marker.

Typing `/` in the editor (at the start or after a space) opens a Notion-style
command palette: keep typing to filter (`/h1`, `/todo`, `/bold`…), arrows to
pick, `enter` to apply, `esc` to dismiss. `/page` turns the draft's text into
a new sub-page of the current page and jumps straight into it (empty draft →
it asks for the title). `/table` scaffolds a markdown table; `i` on any
table edits it as a markdown grid — cell edits update rows in place, while
adding/removing rows or columns rebuilds the table (the API fixes a table's
shape at creation). Pages with covers render them at the top of the
view; page icons are intentionally not shown in the page list (`I` still
sets them for the Notion apps). Block-type commands appear for new
blocks only — the API can't retype an existing block. `E` opens the page as markdown in `$VISUAL`/`$EDITOR` (default
`vim`); on save you confirm before the page content is replaced in Notion.
The markdown round-trip covers headings, lists, to-dos, quotes, code, tables,
dividers and external images — callouts, columns, uploaded images, synced
blocks and embeds are simplified, and the confirm prompt warns when a page
contains them. Sub-pages are never deleted by a replace.

## Theming

```toml
accent = "205"        # UI chrome color: ANSI-256 index or "#ff79c6"
style  = "dracula"    # markdown theme: dark, light, dracula, tokyo-night,
                      # pink, ascii, notty — or a path to a glamour JSON
```

Unset, the accent stays the default blue and the markdown theme follows
your terminal background (dark/light).

## Images

In kitty and Ghostty, images render at full pixel resolution via the kitty
graphics protocol's unicode placeholders — they scroll and clip like text.
Every other terminal gets a truecolor half-block mosaic. Auto-detected;
override in config:

```toml
images = "auto"    # default: sharp on kitty/ghostty, mosaic elsewhere
# images = "mosaic"  # force the fallback
# images = "pixels"  # force the graphics protocol
```

Works inside tmux too, provided tmux forwards the escapes — add to
`~/.tmux.conf` (checked automatically; without it the mosaic is used and
the status bar tells you):

```
set -g allow-passthrough on
```

## Caching

Fetched pages persist to `~/.cache/lazynotion/` (or `$XDG_CACHE_HOME`), so
revisited pages open instantly — including across restarts. A single cheap
metadata request then checks whether the page changed in Notion and refetches
only if it did ("updated from notion" appears in the status bar when that
happens). Sub-pages of whatever you're reading prefetch in the background.
Edits made in the TUI invalidate the affected page; `r` always forces a full
refetch. Delete the cache directory any time — it rebuilds itself.

## Roadmap

- [x] M1 — app shell, auth, workspace search sidebar
- [x] M2 — page content rendering (blocks → markdown → glamour)
- [x] Editing — block cursor, to-do toggle, inline edit, append, `$EDITOR` round-trip
- [x] M4 — inline images (truecolor half-block mosaic, works in any modern terminal)
- [x] M3 — child-page navigation, disk cache + prefetching (database lists still open)
- [x] M4b — pixel-perfect images on kitty/Ghostty (unicode placeholders)
- [ ] M5 — theming, help overlay
