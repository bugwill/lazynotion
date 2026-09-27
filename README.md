# lazynotion

A lazygit-style TUI for browsing your Notion workspace.

## Install

Homebrew (macOS or Linux):

```sh
brew install justinm35/tap/lazynotion
```

Or grab a prebuilt binary from the
[releases page](https://github.com/justinm35/lazynotion/releases)
(darwin/linux, amd64/arm64).

Or with [Go](https://go.dev/dl/) 1.25 or newer:

```sh
go install github.com/justinm35/lazynotion/cmd/lazynotion@latest
```

This drops a `lazynotion` binary into `$(go env GOPATH)/bin` (usually
`~/go/bin`) — make sure that's on your `PATH`. Or run from a clone:

```sh
git clone https://github.com/justinm35/lazynotion
cd lazynotion && go run ./cmd/lazynotion
```

Works on macOS and Linux, in any modern terminal (kitty and Ghostty get
pixel-perfect images; everything else gets a good fallback).

## Setup

```
lazynotion auth
```

walks you through it: it opens Notion's integration dashboard, prompts
for the token, validates it live, and saves it to the config — run it
once per workspace. The one manual step Notion requires: share the pages
you want to browse with the integration (page menu → Connections → your
integration; sharing a top-level page includes everything nested in it).

<details>
<summary>Manual setup</summary>

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

</details>

To keep the sidebar to top-level pages only (nested pages are reached by
navigating into their parents, or via `/` search, which always looks
everywhere):

```toml
root_pages_only = true   # global, or per-workspace inside [workspaces.x]
```

Note: with this on, a nested page whose parent isn't shared with the
integration can only be reached through search.

The Pages list appears at the root. Opening a page, database, or Recent takes
over the full view; `Esc` walks back through opened content and then returns to
Pages.

The first sidebar entry is `Recent`, followed by the usual Pages list.
Recent shows up to 100 pages and databases shared with the integration,
ordered by last edit (newest first), with an edit timestamp for each entry.
It includes nested pages regardless of `root_pages_only`. Use `j/k` and
`Enter` to open an entry; `Esc` returns to Recent, and another `Esc` returns
to the home screen. To start inside Recent:

```toml
root_pages = "recent"   # global, or per-workspace inside [workspaces.x]
```

Recent means recently modified content, not your Notion browsing history.
A light-blue progress bar at the bottom of Pages shows the current query stage
and completed work. Pagination with an unknown total uses an animated bar.
The bar also appears during cached content updates and hides when work ends.
Recent opens its cached local index immediately. A successful snapshot stays
fresh for 60 seconds; reopening it during that window makes no Recent requests.
`r` bypasses that window and joins an existing refresh rather than starting
another job. The first Search batch appears before supplementary work ends,
and later batches update the list while preserving the selected entry.

The index retains all discovered metadata and parent/child links, with the
newest 100 entries displayed. Normal refreshes search the latest 100 indexed
objects, retrieve remembered visible ordinary pages omitted by Search, and query
data-source rows modified since each source's last successful sync. A two-minute
overlap and ID deduplication cover timestamp boundaries. Initial and deep row
queries take the newest 100 rows per source, which suffice for the global top 100.

Opening Recent and pressing `r` never start or resume recursive discovery.
Press `R` explicitly to paginate Search and discover ordinary sub-pages
recursively, including child databases. Database row bodies are not traversed.
Quick refreshes remain available after an interrupted deep scan. Results and discovery
checkpoints are saved periodically, so an interrupted scan can resume. Deep sync revalidates visible cached rows to detect deletion;
incremental queries alone cannot report deleted rows. Database entries use their
containing database's title and edit timestamp, not schema timestamps.

Partial failures retain successful results and leave failed-source checkpoints
unchanged for a later retry. Switching workspaces cancels the old job. Metadata
learned while browsing and changes made in the TUI also update the local index.
Independent lookups run with at most four tasks in flight through a conservative
shared 3 requests/second limiter. Rate-limited requests respect `Retry-After`;
cursor-dependent pagination remains sequential. Only integration-accessible
content is included. Search indexing and discovery limits mean results cannot
be guaranteed to match Notion's native Library, and tied edit times can differ.

## Run

```sh
lazynotion
```

`lazynotion auth` connects a workspace (see Setup above); after that,
plain `lazynotion` starts the TUI.

## Keys

| Key | Action |
| --- | --- |
| `j` / `k` | move through Pages at the root, list entries, or blocks in a page |
| `enter` | open page/sub-page · fold toggles · elsewhere: stamp a new empty block below |
| `/` | search workspace (sidebar) / find in page (viewer) |
| `]` / `[` | next / previous find match |
| `u` | undo last delete, edit, or toggle (in the viewer) |
| `w` | switch workspace (sidebar) |
| `?` | help overlay |
| `space` | toggle to-do under cursor |
| `i` / `e` | edit block in place (`esc` saves, `ctrl+d` or `ctrl+c` discards) |
| `n` / `N` | viewer: new block below / above cursor · sidebar (`n`): new page under the highlighted one |
| `J` / `K` | move block down / up (recreates a block — comments on it are lost) |
| `d` | delete block under cursor (goes to Notion's Trash) |
| `a` | append a paragraph after cursor |
| `esc` | back through opened content, then return to Pages |
| `E` | edit whole page in `$EDITOR` |
| `r` | viewer: refresh current page · sidebar: refresh everything |
| `y` / `Y` | yank block content (markdown) · copy block/page link |
| `v` | visual select: `j`/`k` extend across blocks, `y` yanks them all |
| `I` | set the page icon: an emoji (`🎯`) or a Notion built-in (`target red`) |
| `ctrl+o` | open page in browser |
| mouse wheel | scroll the visible list or page |
| click page title | rename the page; Enter saves, Esc cancels |
| mouse press / drag | select a word / text range while reading (Whip: long-press, then drag) |
| `b` with selected reading text | make the selected text bold and save automatically |
| `q` / `ctrl+c` | quit outside the inline editor |

## Editing

Text can also be formatted while reading: long-press and drag in Whip's TUI
mode to highlight text, then open the keyboard with its toolbar button and
press `b` to make the selection bold and save it to Notion. This preserves
links and other formatting, supports selections across text blocks, and never
inserts a new block. `esc` cancels a text selection; `u` undoes saved formatting.
The selection's underline is a temporary screen overlay, never saved to Notion.
Click outside a selection to cancel it; clicking inside keeps the range.
In lists, Whip swipes move the cursor in the finger direction: down advances
and up goes back. Page content uses standard wheel scrolling, so a downward
swipe moves the content downward and reveals earlier text.
Each wheel event moves one list entry or one line.

Quick edits (`space`, `i`, `n`, `a`) apply instantly and sync to Notion in
the background. `i` swaps the block under the cursor for an inline editor
right where it sits — type (multi-line works, `enter` adds a line), then
`esc` to save or `ctrl+d` (also `ctrl+c`) to discard. To format existing
content, drag with the left mouse button to select a range, or press `ctrl+v`
and move the cursor with arrows. You can also
press `ctrl+w` to select the word at the cursor; `b` toggles bold, `u` undoes
that formatting, and `ctrl+z` undoes the latest edit. `shift+enter` (or
`alt+enter` in
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

Sidebar browse/search lists, the Recent list, and database schemas with their first batch of
rows also persist across restarts. These caches are scoped to the integration
and list filter, display immediately, and refresh in the background (Recent
uses the 60-second freshness window described above). Further
database batches are fetched as you navigate. Cached content remains readable
if the background request fails. Cache files contain page content, never API
tokens, and are written with owner-only permissions.

## Roadmap

- [x] M1 — app shell, auth, workspace search sidebar
- [x] M2 — page content rendering (blocks → markdown → glamour)
- [x] Editing — block cursor, to-do toggle, inline edit, append, `$EDITOR` round-trip
- [x] M4 — inline images (truecolor half-block mosaic, works in any modern terminal)
- [x] M3 — child-page navigation, disk cache + prefetching (database lists still open)
- [x] M4b — pixel-perfect images on kitty/Ghostty (unicode placeholders)
- [x] M5 — theming, help overlay, guided auth (`lazynotion auth`)
- [ ] M6 — databases: browse, sort, and edit rows
