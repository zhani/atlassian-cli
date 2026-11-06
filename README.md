# atlassian-cli

<div align="center">
  <img src="docs/images/external/logo/github-banner.png" alt="Logo">
</div>

<div align="center">
  <em>A lightweight, native Go CLI for Atlassian Jira and Confluence</em>
</div>

<br />

<div align="center">
  <a href="https://github.com/zhani/atlassian-cli/releases">
    <img src="https://img.shields.io/github/v/release/zhani/atlassian-cli?color=3670A0" alt="Release">
  </a>
  <a href="https://github.com/zhani/atlassian-cli/commits/main">
    <img src="https://img.shields.io/github/commit-activity/m/zhani/atlassian-cli" alt="Commit activity">
  </a>
  <a href="https://opensource.org/licenses/MIT">
    <img src="https://img.shields.io/badge/License-MIT-yellow.svg?color=3670A0" alt="License: MIT">
  </a>
</div>

<br />

## Installation

This is a fork of [putcho01/atlassian-cli](https://github.com/putcho01/atlassian-cli)
with an interactive TUI (issue browser, explorers, Confluence page tree,
bookmarks), more Jira/Confluence commands and MCP tools.

### go install

Requires Go 1.25+:

```bash
go install github.com/zhani/atlassian-cli@latest
```

> If `$GOPATH/bin` is not in your `$PATH`, add it to your shell config:
> `export PATH="$PATH:$(go env GOPATH)/bin"`

### Prebuilt binaries

Download an archive for Linux, macOS or Windows (amd64/arm64) from the
[releases](https://github.com/zhani/atlassian-cli/releases), unpack it and put
`atlassian-cli` on your `PATH`. The binaries are static and need nothing else.

### From source

```bash
git clone https://github.com/zhani/atlassian-cli.git
cd atlassian-cli
go build -o atlassian-cli .     # or: go install .  (to $(go env GOPATH)/bin)
```

### Building without internet access

On a machine with access, bundle the dependencies with the source and copy the
archive over:

```bash
go mod vendor                                    # dependencies into ./vendor
tar czf atlassian-cli-src.tgz --exclude=.git .
```

On the offline machine, `go build -o atlassian-cli .` then uses `./vendor`
(Go 1.25+ must be installed there; an older Go would try to download a newer
toolchain). Alternatively cross-compile a binary and copy just that:
`CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o atlassian-cli .`

## Quick Start

### Cloud (atlassian.net)

1. [Create an API Token](https://id.atlassian.com/manage-profile/security/api-tokens)

2. Set environment variables:

```bash
export JIRA_URL=https://your-domain.atlassian.net
export JIRA_EMAIL=you@example.com
export JIRA_API_TOKEN=your-api-token
```

3. Verify authentication:

```bash
atlassian-cli jira myself
```

To use Confluence as well, set the additional variables:

```bash
export CONFLUENCE_URL=https://your-domain.atlassian.net/wiki
export CONFLUENCE_EMAIL=you@example.com
export CONFLUENCE_API_TOKEN=your-api-token
```

### Server/Data Center

```bash
export JIRA_URL=https://jira.example.com
export JIRA_PERSONAL_TOKEN=your-pat
```

> If `JIRA_EMAIL` is not set, Bearer (PAT) authentication is used automatically.

## Authentication

Two authentication methods are supported:

### Cloud - API Token (Basic Auth)

Used with Atlassian Cloud (`*.atlassian.net`). Authenticates via Basic auth using your email address and API token.

| Variable | Description |
|----------|-------------|
| `JIRA_URL` | Jira Cloud URL (e.g. `https://your-domain.atlassian.net`) |
| `JIRA_EMAIL` | Your Atlassian account email address |
| `JIRA_API_TOKEN` | [API Token](https://id.atlassian.com/manage-profile/security/api-tokens) |
| `CONFLUENCE_URL` | Confluence Cloud URL (e.g. `https://your-domain.atlassian.net/wiki`) |
| `CONFLUENCE_EMAIL` | Your Atlassian account email address |
| `CONFLUENCE_API_TOKEN` | API Token |

### Server/Data Center - Personal Access Token (Bearer)

Used with self-hosted Jira/Confluence Server/DC. Authenticates via Bearer auth using a PAT.

| Variable | Description |
|----------|-------------|
| `JIRA_URL` | Jira base URL (e.g. `https://jira.example.com`) |
| `JIRA_PERSONAL_TOKEN` | Personal Access Token |
| `CONFLUENCE_URL` | Confluence base URL |
| `CONFLUENCE_PERSONAL_TOKEN` | Personal Access Token |

> If `EMAIL` is set, Basic auth (Cloud) is used; otherwise Bearer auth (Server/DC) is used.

### Shared credentials

On Cloud you can set the credentials once with `ATLASSIAN_*` variables. Product-specific variables (`JIRA_*`, `CONFLUENCE_*`) always take precedence.

| Variable | Description |
|----------|-------------|
| `ATLASSIAN_URL` | Site URL. Used as `JIRA_URL`; on `*.atlassian.net` also as `CONFLUENCE_URL` (+ `/wiki`) |
| `ATLASSIAN_EMAIL` | Account email (only combined with an API token) |
| `ATLASSIAN_API_TOKEN` | API token (Basic auth) |
| `ATLASSIAN_PERSONAL_TOKEN` | Personal access token (Bearer auth) |

On Server/DC, Confluence runs on its own host, so `CONFLUENCE_URL` is still required there.

Only the variables for the service you use are required. For example, if you only use Jira, you don't need to set the Confluence variables.

### Optional settings

These apply to Cloud and Server/DC alike.

| Variable | Description |
|----------|-------------|
| `JIRA_DEFAULT_PROJECT` | Default project for `--project` / `-p` (`-p ""` searches all projects) |
| `JIRA_MAX_RESULTS` | Default of `--max-results` for `jira issue list`/`search` and the explorers (built-in 500); the TUIs load that many up front. A `--max-results` flag still wins |
| `JIRA_DEFAULT_BOARD` | Agile board ID for `jira sprint` |
| `JIRA_FEATURE_TYPE` | Issue type of features for `jira feature` (default `New Feature`) |
| `JIRA_FEATURE_LINK_TYPES` | Comma-separated link phrases from a feature to its issues (default `is implemented by,gets values from`) |
| `JIRA_EPIC_LINK_FIELD` | JQL field linking issues to epics (default `parent` on Cloud, `Epic Link` on Server/DC) |
| `JIRA_DETAIL_FIELDS` | Comma-separated custom fields (names or IDs) shown in the TUI issue view, in this order (default: all non-empty) |
| `ATLASSIAN_MIRROR` | Offline mirror directory to use instead of the servers (`off` disables auto-detection; see [Offline mirror](#offline-mirror)) |
| `ATLASSIAN_BOOKMARKS` | Bookmark file (default `~/.config/atlassian-cli/bookmarks.yaml`) |
| `ATLASSIAN_CA_FILE` | Extra CA certificates to trust (PEM, one or more, or a single DER `.crt`), on top of the system ones. For a private CA, and on macOS also for server certificates the system rejects whatever the keychain trusts (e.g. valid for more than 825 days): with this set, the certificate is checked against the file when the system check fails. If it is a chain, include the issuing (intermediate) CA |
| `ATLASSIAN_IMAGES` | How the Confluence browser draws images: `auto` (default), `kitty`, `iterm`, `sixel`, `blocks` or `off` |
| `ATLASSIAN_EMBEDS` | Embeds file of `mirror export` and the Confluence browser (default `~/.config/atlassian-cli/embeds.yaml`; see [Embedded documents](#embedded-documents)) |
| `ATLASSIAN_SLIDES_CACHE` | Where the Confluence browser keeps rendered PDF slides (default `~/.cache/atlassian-cli/slides`) |
| `GLAMOUR_STYLE` | Colour theme of rendered issues and pages (`dark`, `light`, `dracula`, …; default from the terminal background) |
| `VISUAL` / `EDITOR` | Editor for `-e`, `confluence page edit` and `bookmark edit` (default `vi`) |

## Commands

### Jira

```bash
# Authentication
atlassian-cli jira myself                # Show authenticated user
atlassian-cli version                    # Print the version

# Issues
atlassian-cli jira issue get PROJ-123    # Get issue (includes description)
atlassian-cli jira issue open PROJ-123   # Open issue in browser
atlassian-cli jira issue search "project = PROJ"       # Search issues via JQL (interactive browser in a terminal)
atlassian-cli jira issue search "project = PROJ" -i    # Always open the interactive browser
atlassian-cli jira issue search "project = PROJ" --plain --max-results 200   # Plain table, paginated
atlassian-cli jira issue list -a me -s~Done                     # List issues with filter flags (see below)
atlassian-cli jira issue create --project PROJ --summary "New task"  # or omit --project if JIRA_DEFAULT_PROJECT is set
atlassian-cli jira issue create --summary "Crash" --type Bug --template desc.md   # Description from a Markdown file
atlassian-cli jira issue create --summary "Crash" -e                              # Write the description in $EDITOR
atlassian-cli jira issue update PROJ-123 --field summary="Updated summary"
atlassian-cli jira issue delete PROJ-123
atlassian-cli jira issue get PROJ-123 --fields status,"Story Points"   # Selected fields, by ID or name
atlassian-cli jira issue subtasks PROJ-123
atlassian-cli jira issue transition PROJ-123 "In Progress"
atlassian-cli jira issue assign PROJ-123 me             # me, x (unassign), default, or a user name/email
atlassian-cli jira issue clone PROJ-123 -s "Same on Android" -H "iOS:Android"   # -H find:replace in summary/description
atlassian-cli jira issue delete PROJ-123 --cascade      # also delete subtasks
atlassian-cli jira issue development PROJ-123           # Linked pull requests and branches (alias: dev)

# Links
atlassian-cli jira issue link PROJ-1 PROJ-2 Blocks              # PROJ-1 blocks PROJ-2
atlassian-cli jira issue link FEAT-1 PROJ-3 "is implemented by" # type name or either phrase
atlassian-cli jira issue unlink PROJ-1 PROJ-2 [--type Blocks]
atlassian-cli jira issue link remote PROJ-1 https://example.com/spec "Spec"
atlassian-cli jira issue link types

# Time tracking
atlassian-cli jira issue worklog add PROJ-123 "2h 30m" --comment "Code review"

# Users and fields
atlassian-cli jira user search jane
atlassian-cli jira field list --custom "story"           # Field IDs for --field / --fields / --columns

# Comments
atlassian-cli jira issue comment list PROJ-123
atlassian-cli jira issue comment add PROJ-123 --body "Looks **good** to me"
echo "Deployed to staging" | atlassian-cli jira issue comment add PROJ-123     # Body from stdin
atlassian-cli jira issue comment add PROJ-123                                 # Opens $EDITOR in a terminal

# Attachments
atlassian-cli jira issue attachment list PROJ-123                          # List attachments on an issue
atlassian-cli jira issue attachment get 10123                             # Show a single attachment's metadata by ID
atlassian-cli jira issue attachment download PROJ-123 design.pdf         # Download by filename (saved as ./design.pdf)
atlassian-cli jira issue attachment download PROJ-123 10123 --file out.pdf  # Download by attachment ID to a path
atlassian-cli jira issue attachment download PROJ-123 design.pdf --file - # Stream to stdout
atlassian-cli jira issue attachment upload PROJ-123 shot.png log.txt      # Attach files
atlassian-cli jira issue attachment delete PROJ-123 shot.png              # By filename or ID

# Xray tests (Server/DC): `issue get -o markdown` and the issue browser show a Test Details
# section — the manual steps with each step's latest result, comment and evidence
atlassian-cli jira issue evidence PROJ-42 --list                          # Evidence + step attachments
atlassian-cli jira issue evidence PROJ-42 --dir /tmp/evidence             # Download them (or name some)
```

### Listing issues with filters

`jira issue list` builds the JQL for you. The project defaults to `JIRA_DEFAULT_PROJECT`.

| Flag | Filter |
|------|--------|
| `-p, --project` | Project key (default `JIRA_DEFAULT_PROJECT`; `-p ""` for all projects) |
| `-s, --status` | Status |
| `-a, --assignee` | Assignee: a user, `me`, or `x` (unassigned) |
| `-r, --reporter` | Reporter: a user, `me`, or `x` |
| `-t, --type` | Issue type |
| `-y, --priority` | Priority |
| `-R, --resolution` | Resolution; `Unresolved` for open issues, `~Unresolved` for resolved ones |
| `-l, --label` | Label (repeatable) |
| `-w, --watching` | Issues you watch |
| `--created`, `--updated` | Since a period: `-7d`, `2w`, `-12h`, `today`, `week`, `month`, `year` |
| `--order-by`, `--reverse` | Sort field (default `created`, descending) |
| `-q, --jql` | Extra JQL, ANDed with the filters |

Prefix any value with `~` to negate it. `--show-jql` prints the query without running it.

```bash
atlassian-cli jira issue list -a me -R Unresolved -p ""           # My open issues in all projects
atlassian-cli jira issue list -a me -s~Done                       # Not done, in the default project
atlassian-cli jira issue list -t Bug -y High --created week       # High-priority bugs from this week
atlassian-cli jira issue list -a x -l backend -l~wontfix          # Unassigned backend issues
atlassian-cli jira issue list -q "summary ~ crash" --show-jql     # Inspect the generated JQL
```

### Writing descriptions and comments

Issue descriptions and comments are written in **Markdown** and converted to Jira wiki markup (headings, emphasis, code blocks, links, lists, tables, quotes). Pass `--raw` to send Jira wiki markup unchanged.

The text can come from:

- the flag itself (`--description`, `--body`)
- `--template FILE` (`-` reads stdin)
- `-e, --editor` to write it in `$VISUAL` / `$EDITOR` (default `vi`)
- for required text (comment body, Confluence page update): piped stdin, or `$EDITOR` when run in a terminal

An optional description never reads stdin implicitly, so `issue create` inside a `while read` loop does not consume the loop's input.

### Explorers

Features, epics and sprints open in a two-pane explorer in a terminal: the
list on the left, the issues of the selected entry on the right (with all
issue browser keys). `tab` switches panes, `ctrl+h` / `ctrl+l` focus the left /
right one; in the issue pane `l` opens the
issues of an epic and `h` goes back. With `--plain`, `-o` or a pipe they
print instead.

```bash
# Features: issues of type JIRA_FEATURE_TYPE with the issues linked by JIRA_FEATURE_LINK_TYPES
atlassian-cli jira feature list                         # explorer; Link column shows how each issue is linked
atlassian-cli jira feature list -a me -s "In Progress"   # issue-list filter flags apply to the features
atlassian-cli jira feature list FEAT-12                  # one feature's linked issues
atlassian-cli jira feature list --plain -o csv           # feature,key,link,type,status,assignee,summary rows
atlassian-cli jira feature list --type Initiative --link-type "is parent of"   # override the configuration

# Epics
atlassian-cli jira epic list                             # explorer
atlassian-cli jira epic list EPIC-7                      # issues of an epic
atlassian-cli jira epic add EPIC-7 PROJ-1 PROJ-2
atlassian-cli jira epic remove PROJ-2

# Sprints (board: --board, JIRA_DEFAULT_BOARD, or the project's only Scrum board)
atlassian-cli jira sprint list                           # explorer, starting at the active sprint
atlassian-cli jira sprint list --current -a me           # my issues in the active sprint (--prev, --next)
atlassian-cli jira sprint list 42 --plain
atlassian-cli jira sprint add 42 PROJ-1 PROJ-2
```

Resolved features and epics are hidden unless `--all` is given (or `-R` picks
a resolution).

### Interactive issue browser

`jira issue search`, `jira issue list`, the explorers and `bookmark open` show
issues in an interactive browser when run in a terminal without an explicit
`-o`/`--output` (use `--plain` to get the plain table, or `-i` to force the
browser). Piped output is never interactive.

| Key | Action |
|-----|--------|
| `↑`/`↓`, `j`/`k` | Move |
| `g` / `G` | Top / bottom |
| `ctrl+f` / `ctrl+b`, `PgDn` / `PgUp` | Page down / up |
| `enter` / `v` | View issue (description, custom fields, links, subtasks, latest comments) |
| `esc` | Back / clear filter |
| `/` | Fuzzy-find the loaded issues (fzf-style across all columns, best first): `↓`/`↑`, `ctrl+j`/`ctrl+k` or `ctrl+n`/`ctrl+p` move, `enter` opens the issue and keeps the list filtered, `esc` clears |
| `m` | Transition (move) the issue |
| `o` | Open in browser |
| `c` / `C` | Copy issue URL / key to the clipboard |
| `ctrl+r` / `F5` | Refresh |
| `i` | Detail view: show the issue's images full screen — Xray evidence step by step, step attachments, image attachments (`n`/`p` next/previous, `q` back). The detail view also draws them in place (kitty placeholders, or coloured blocks elsewhere), like the Confluence browser, with images in the description and an Attachments list |
| `l` | Detail view: follow a link — pick from the parent, epic, linked issues, subtasks, Xray tests covering it and issues mentioned in its text; `enter` opens it, `esc` walks back to where you came from. With Confluence configured the list also has the pages the issue links to (its "wiki page" remote links, page links in its description and comments): `enter` opens the page in the page browser, `q` there comes back |
| `b` | Bookmark the issue (again: remove the bookmark) |
| `B` | Bookmarks: pick a bookmarked issue, query, feature, epic or sprint to show here |
| `S` | Bookmark the list's query |
| `?` | Toggle full help |
| `q` | Quit |

The issue view shows custom fields by their display names: longer text fields
(e.g. reproduction steps, comment logs) and checklists as their own sections,
short values in a Fields list. Set `JIRA_DETAIL_FIELDS` to show only selected
fields, e.g. `JIRA_DETAIL_FIELDS="Steps to Reproduce,Comment log"`.

Copying uses `pbcopy`, `xclip`, `xsel` or `wl-copy` when available and falls
back to the OSC 52 terminal escape sequence otherwise. Set `GLAMOUR_STYLE`
(e.g. `dark`, `light`, `dracula`) to override the issue view's colour theme.

### tmux with vim-tmux-navigator

[vim-tmux-navigator](https://github.com/christoomey/vim-tmux-navigator) binds
`ctrl+h/j/k/l` in tmux and passes them on only to vim, neovim and fzf, so
atlassian-cli never sees them (pane focus, `ctrl+j`/`ctrl+k` in filters). Add
atlassian-cli to its list by redefining the bindings in `tmux.conf`, **after**
the line that runs tpm:

```tmux
is_vim="ps -o state= -o comm= -t '#{pane_tty}' \
    | grep -iqE '^[^TXZ ]+ +(\\S+\\/)?g?\\.?(view|l?n?vim?x?|fzf|atlassian-cli)(diff)?(-wrapped)?$'"
bind-key -n 'C-h' if-shell "$is_vim" 'send-keys C-h' 'select-pane -L'
bind-key -n 'C-j' if-shell "$is_vim" 'send-keys C-j' 'select-pane -D'
bind-key -n 'C-k' if-shell "$is_vim" 'send-keys C-k' 'select-pane -U'
bind-key -n 'C-l' if-shell "$is_vim" 'send-keys C-l' 'select-pane -R'
```

Reload with `tmux source-file ~/.config/tmux/tmux.conf` (or your config path).
As in vim, `ctrl+h`/`ctrl+l` at the edge of atlassian-cli move on to the next tmux
pane: from the right pane of a two-pane view `ctrl+l` leaves it, from the left one
`ctrl+h`; the one-pane issue and bookmark browsers pass both on. `ctrl+j`/`ctrl+k`
always move to the tmux pane below/above, except in a filter, where they move
between matches.
Without it, `tab` still switches panes and `ctrl+n`/`ctrl+p` move in filters.

### Bookmarks

Bookmarks keep the work you return to one keystroke away: Jira issues, saved
JQL queries, features, epics, sprints and Confluence pages.

```bash
atlassian-cli bookmark                     # bookmark browser (in a terminal)
atlassian-cli bookmark add PROJ-123        # an issue
atlassian-cli bookmark add query "assignee = currentUser() AND resolution = Unresolved" --name "My open work"
atlassian-cli bookmark add feature PROJ-7  # also: epic KEY, sprint ID, page ID
atlassian-cli bookmark list                # numbered; -o json for scripts
atlassian-cli bookmark open "My open work" # by number, name, key or ID; --web for the browser
atlassian-cli bookmark rename 3 "Payments"
atlassian-cli bookmark remove 3
atlassian-cli bookmark edit                # the YAML file in $EDITOR
atlassian-cli bookmark path                # where the file is
```

In the bookmark browser `enter` opens a bookmark in its view: an issue in the
issue view, a query in the issue browser, a feature, epic or sprint as its
issues, a page in the page tree. Quitting that view returns to the bookmarks.
`o` opens the bookmark on the web, `c` / `C` copy its link / key, `r`
renames, `d` deletes. `/` fuzzy-finds bookmarks (fzf-style, across kind,
key/query, name and title; best match first): `↓`/`↑`, `ctrl+j`/`ctrl+k` or `ctrl+n`/`ctrl+p` move,
`enter` opens the selected bookmark, `esc` goes back to the full list.

Press `b` in the issue browser, on a feature/epic/sprint in the explorers or on
a page in `confluence browse` to bookmark it (`b` again removes it); `S` in an
issue list bookmarks its query. `B` in those browsers opens your bookmarks
without leaving them: in `confluence browse` the page bookmarks (the tree jumps
to the chosen page, loading its space or switching to it), in the issue browser
and explorers the issue, query, feature, epic and sprint bookmarks (an issue
opens in the issue view, the others replace the list). Bookmarks live in
`~/.config/atlassian-cli/bookmarks.yaml` (or `ATLASSIAN_BOOKMARKS`), a plain
YAML file you can edit, back up or sync.

### Confluence page browser

`confluence browse [SPACE]` shows the page tree on the left and the selected
page rendered on the right; child pages load when a page is expanded. With Jira
configured, a Jira issue list on a page (the Jira issues macro with a query, a
filter) shows as a table of the issues it finds, with the macro's columns and
limit (at most 1000), as in the browser; without Jira, its query. Rich-text
columns (description, text-area custom fields) show formatted: on their own
when they are the list's only columns, else after the table under each key.

| Key | Action |
|-----|--------|
| `↑`/`↓`, `j`/`k`, `g`/`G`, `ctrl+f`/`ctrl+b` | Move in the tree (or scroll the page when it has focus) |
| `enter` / `l` | Expand the page (again: step into its children) |
| `h` | Collapse, or go to the parent |
| `tab` / `w` | Switch between tree and page |
| `ctrl+h` / `ctrl+l` | Focus the tree (left) / the page (right) |
| `/` | Fuzzy-find pages (the whole space is loaded in the background): `↓`/`↑`, `ctrl+j`/`ctrl+k` or `ctrl+n`/`ctrl+p` move between matches, `enter` jumps to the page, `esc` goes back |
| `i` | View the page's images full screen (every slide of an embedded PDF) |
| `[` / `]` | Previous / next slide of the embedded PDF in view |
| `L` | Links on the page: Jira issues (Jira issue macros, links to issues, then the issues of its issue lists; with Jira configured) — `enter` opens one in the issue browser, `q` there comes back — then the Confluence pages it links to — `enter` selects that page in the tree, loading its space when needed |
| `b` | Bookmark the page (again: remove the bookmark) |
| `B` | Bookmarks: jump to a bookmarked page |
| `o` / `c` | Open the page in the browser / copy its URL |
| `ctrl+r` / `F5` | Reload the page |
| `?` / `q` | Help / quit |

### Images in the Confluence browser

`confluence browse` shows the images of a page in place, and `i` opens them full
screen one at a time (`n`/`p` move between them, `esc` goes back). How they are
drawn depends on the terminal, which is detected automatically (inside tmux by
asking tmux about the outer terminal):

| Terminal | In the page | Full screen (`i`) |
|----------|-------------|-------------------|
| kitty, Ghostty (also inside tmux) | Real image, scrolls with the text (Unicode placeholders) | Real image |
| WezTerm | Colored half-block characters | Real image (iTerm2 protocol) |
| foot, other Sixel terminals | Colored half-block characters | Real image (Sixel) |
| Anything else | Colored half-block characters | Half-block characters |

Inside tmux, real images need `set -g allow-passthrough on` in `~/.tmux.conf`
(tmux 3.3+). Sixel goes through tmux without it when tmux has Sixel support
(3.4+) and knows the outer terminal can show Sixel; otherwise tmux prints
"SIXEL IMAGE" instead, and WezTerm uses the iTerm2 protocol through passthrough.
To let tmux draw Sixel itself, declare the feature, e.g.
`set -as terminal-features 'xterm-256color:sixel'` (applies to every client
with that `TERM`). Set `ATLASSIAN_IMAGES` to `kitty`, `iterm`, `sixel`, `blocks` or `off`
to override the detection.

Known issue: the full-screen view stays blank with WezTerm on Windows connected
over SSH to tmux (WezTerm on Linux works); see [TODO.md](TODO.md).

An embedded PDF (Confluence's PDF macro, `viewpdf`, or a local copy mapped in the
[embeds file](#embedded-documents)) shows as a slide show in its
place: one page at a time, `[`/`]` turn the pages, and `i` shows every page full
screen. The pages are rendered with MuPDF's `mutool` or, failing that, Poppler's
`pdftoppm` (`apt install mupdf-tools` / `poppler-utils`, `brew install mupdf`
/ `poppler`), once per PDF: they are cached under `~/.cache/atlassian-cli/slides`
(`ATLASSIAN_SLIDES_CACHE`). Without either the page shows the PDF's caption
and the error; the PDF stays in the page's attachments either way.

### Confluence

```bash
# Pages
atlassian-cli confluence page get 12345                       # Get page content
atlassian-cli confluence page get 12345 --content-only        # Only the body, as Markdown (--raw: storage XHTML)
atlassian-cli confluence page create --space PROJ --title "New Page" --body "Hello **world**"
atlassian-cli confluence page create --space PROJ --title "Child Page" --parent 12345 --template child.md   # - reads stdin
atlassian-cli confluence page create --space PROJ --title "Raw" --body "<p>Hello</p>" --raw   # storage format as-is
atlassian-cli confluence page update 12345 --title "Updated Title" --template page.md           # version auto-detected
atlassian-cli confluence page update 12345 --title "Updated Title" --body "New" --version 3     # explicit version
atlassian-cli confluence page edit 12345                      # Edit as Markdown in $EDITOR
atlassian-cli confluence page list --space PROJ               # Pages of a space
atlassian-cli confluence page children 12345                  # Child pages
atlassian-cli confluence page versions 12345                  # Version history
atlassian-cli confluence page copy 12345 --title "New" [--space OPS] [--parent 678]   # Content only
atlassian-cli confluence page move 12345 678                  # Move under page 678 (same space)

# Search
atlassian-cli confluence search --text "release notes" --space PROJ
atlassian-cli confluence search 'space = PROJ AND label = "howto"'   # CQL
atlassian-cli confluence space list

# Browse interactively (see "Confluence page browser" above)
atlassian-cli confluence browse          # all spaces
atlassian-cli confluence browse PROJ     # one space

# Labels
atlassian-cli confluence label list 12345
atlassian-cli confluence label add 12345 important,reviewed
atlassian-cli confluence label remove 12345 outdated

# Page Restrictions
atlassian-cli confluence restriction list 12345
atlassian-cli confluence restriction add 12345 --operation update --type user --name <account-id>
atlassian-cli confluence restriction remove 12345 --operation update --type user --name <account-id>

# Attachments
atlassian-cli confluence attachment list 12345                            # List attachments on a page
atlassian-cli confluence attachment download 12345 diagram.png           # Download by title (saved as ./diagram.png)
atlassian-cli confluence attachment download 12345 att456 --file out.png  # Download by attachment ID to a path
atlassian-cli confluence attachment upload 12345 diagram.png             # Same name: new version
atlassian-cli confluence attachment delete 12345 diagram.png             # By title or ID
```

## Offline mirror

Copy Jira boards, queries and bookmarks and Confluence pages, with every
attachment and image, to a machine that cannot reach Jira or Confluence, and
use them there with the same commands, TUIs and MCP server, read-only.

On the connected machine (normal credentials):

```bash
atlassian-cli mirror export --out mirror.tar.gz                 # board JIRA_DEFAULT_BOARD + everything bookmarked
atlassian-cli mirror export --out mirror.tar.gz --board 42 --board 43 \
    --jql "project = PROJ AND updated >= -30d" --page 123456     # more boards, queries, page trees
atlassian-cli mirror export --out delta.tar.gz --since -7d       # only issues/pages changed since
atlassian-cli mirror export --out jira.tar.gz --only jira --since -2d        # one side only
atlassian-cli mirror export --out wiki.tar.gz --only confluence --since -14d
```

Jira changes often and Confluence rarely, so `--only jira|confluence` lets
each side have its own schedule (bookmarks of that kind only). Both still go
into the **same** mirror on the offline machine, so links between issues and
pages keep resolving: importing one side leaves the other as it is
(URL, freshness, items), `--prune` prunes only the side the archive covers,
and `mirror status` shows each side's last export and full snapshot.

What goes in: the issues of each board's filter (and its sprints), of each
`--jql` query and of every bookmark (issues, queries, features with their
linked issues, epics with their issues, sprints), the issues of the active
sprints (of the exported boards, else of `JIRA_DEFAULT_PROJECT`; skipped when
none is active; `--no-active-sprint` turns it off), your issues that are
unresolved or resolved as anything but Done/Fixed (`--no-my-issues` turns it
off), the issues one step away from those (parent, subtasks, epic, every
linked issue: "relates to", "is blocked by"…, and the tests in Xray's Test
Coverage; not followed further), and each `--page` / bookmarked page with all its
descendants. Links between the two sides come along one step, so they open
offline: the Confluence pages those issues link to (remote "wiki page" links,
page links in any field: `viewpage.action?pageId=`, `/spaces/…/pages/N`,
`/display/SPACE/Title`, tiny `/x/…` links) and the Jira issues those pages
show (Jira issue macro) or link to — but nothing those bring in turn. The
issue lists on those pages (Jira issues macros with a query) run against Jira
at export: their issues are exported (up to each list's limit, at most 1000)
and each page keeps its lists' results, so a list shows offline as Jira
answered it, even for JQL the mirror can't evaluate (`linkedIssues()`,
ScriptRunner, Xray functions); a list whose query failed shows the error. Issues keep their rendered descriptions, all comments and their
attachments, and Xray tests their latest test runs (step results, comments)
with all evidence and step attachments; pages keep their attachments and the images they link on the
same site. The bookmark file and the field names come along too.

Move the archive over any channel and import it on the offline machine:

```bash
atlassian-cli mirror import mirror.tar.gz            # into ~/.local/share/atlassian-cli/mirror
atlassian-cli mirror import mirror.tar.gz --prune    # also drop issues/pages the snapshot no longer has
atlassian-cli mirror status                          # where, how current (-o json for scripts)
```

Refresh by exporting and importing again. An import **upserts**: each issue,
page, board and space in the archive replaces its old copy as a whole
(attachments included), everything else is kept. Without `--since` an
archive is a full snapshot, and `--prune` (full snapshots only) removes what
it no longer has, listing it first — except items that the mirror's other
items still link to (an issue on a mirrored page, a page an issue links to),
which a one-side snapshot doesn't carry. The archive's bookmarks are added to
yours; yours are never removed. Put your own additions under `local/` in the
mirror directory, in the same layout (`local/jira/issues/KEY/issue.json`, …):
they are served on top of the mirror and never touched by an import.

The mirror is used when `ATLASSIAN_MIRROR` names its directory, or
automatically when no `JIRA_URL`/`CONFLUENCE_URL`/`ATLASSIAN_URL` is set and
the default directory holds one (`ATLASSIAN_MIRROR=off` disables that). No
token is needed. The JIRA_* explorer settings of the exporting machine apply
unless set locally. Searches run locally: JQL with `AND`/`OR`/`NOT`,
`= != ~ !~ < <= > >= IN`, `IS [NOT] EMPTY`, `ORDER BY`, text search
(`text`, `summary`, `description`, `comment`, text custom fields), dates
(`-7d`, `startOfWeek()`, …), `currentUser()` (the exporting user), sprints
(`openSprints()`, IDs, names), `"Epic Link"`, `parent`, custom fields by name
or `cf[ID]` and `issue in linkedIssues(KEY[, "phrase"])`; CQL with `type`,
`space`, `title`, `text`, `id`, `ancestor`, `parent`, `label`,
`lastmodified`, `created`, `creator`. History (`WAS`, `CHANGED`) and other
functions are rejected with an error. Changes (create, transition, comment,
…) are refused: "read-only mirror".

`atlassian-cli mirror serve [--addr 127.0.0.1:18081]` serves the mirror over
HTTP for other tools (Jira at `/`, Confluence at `/wiki`).

### Embedded documents

A page that embeds a document from another site — a Widget Connector showing a
Google Slides deck, say — shows nothing offline, and often the exporting
machine can't reach that site either. Keep a local copy (export the deck as
PDF) and map it in the embeds file, `~/.config/atlassian-cli/embeds.yaml` on the
exporting machine (or `ATLASSIAN_EMBEDS`, or `mirror export --embeds FILE`):

```yaml
- url: https://docs.google.com/presentation/d/1jwmHV7I7qb4viFEZ1Cc2j-j6EDIh8D1JJ7Jo97mWgHs
  file: ~/docs/Feature development introduction.pdf
- url: 1aBcD3fGhIjK   # any part of the URL will do, e.g. the document ID
  file: slides/other.pdf   # relative to the embeds file
```

`mirror export` then attaches the file to every exported page embedding a
matching URL (a macro's `url` parameter; query, fragment and a trailing
`/edit`, `/view`, `/preview`, `/pub`, `/embed` are ignored) and puts it in place
of the embed: a PDF as Confluence's PDF macro, which the page browser shows as
a slide show (see [Images in the Confluence browser](#images-in-the-confluence-browser))
and `page get` as an image link with the embedded URL; an image as an image;
anything else as a link to the attachment. Only the file travels in the
archive; the slides are rendered where the page is viewed. A Widget Connector
with no mapping is reported in the export's warnings, so the log shows what is
still missing. The live pages are not changed. Pages are only re-read when
they changed, so after adding a mapping export once without `--since`.

The page browser (`confluence browse`) reads the same embeds file when it shows
live pages: a mapped embed shows as it would in the mirror (a PDF as a slide
show), read from the local file, so the exporting machine sees the slides
without a mirror. An attachment of the page with the same name wins, as in the
export.

## Output Formats

All commands support three output formats via the `--output` / `-o` flag (issue lists also support `csv`, and `--columns` / `--no-headers` for `table`, `csv` and `markdown`; columns: `key`, `summary`, `status`, `resolution`, `assignee`, `reporter`, `priority`, `type`, `labels`, `created`, `updated`, `project`, `customfield_<id>` or a field name):

```bash
# Default: human-readable table
atlassian-cli jira issue search "project = PROJ" -o table

# CSV with chosen columns, no header row (issue lists)
atlassian-cli jira issue list -a me -o csv --columns key,status,summary --no-headers

# Machine-readable JSON
atlassian-cli jira issue search "project = PROJ" -o json

# GitHub-flavored Markdown (great for Claude Code)
atlassian-cli jira issue search "project = PROJ" -o markdown
```

Jira custom fields (`customfield_*`) returned by the API are preserved: they
appear flattened in `json` output and as extra rows in the `markdown` detail
view for `jira issue get`. (The `table` view keeps its fixed summary columns.)

### Markdown pages

Page bodies are written in **Markdown** and converted to Confluence storage
format; `--raw` sends storage XHTML unchanged. The conversion is the inverse
of the Markdown output, so pages round-trip:

| Markdown | Confluence |
|----------|------------|
| ```` ```lang ```` fenced code | Code macro (language kept) |
| `> **Note:** text` (`Info`, `Warning`, `Tip`) | Note / info / warning / tip panel |
| `![alt](https://…)` / `![alt](file.png)` | Image from URL / attachment |
| Tables, lists, headings, emphasis, links | Same in storage format |
| ```` ```storage ```` fenced block | Copied verbatim (embed any macro) |

`confluence page edit` opens the page as Markdown in `$EDITOR` and saves a new
version. It refuses pages that use features Markdown cannot represent (other
macros, page links and mentions, inline styles, merged cells…) and lists them;
use `--raw` to edit the storage XHTML instead, or `--allow-lossy` to proceed.

### Raw API access

`jira api` and `confluence api` send an authenticated request to any REST
endpoint and pretty-print the JSON response:

```bash
atlassian-cli jira api /rest/api/2/field
atlassian-cli jira api /rest/api/2/search -q jql="project = PROJ" -q maxResults=5
atlassian-cli jira api /rest/api/2/issue/PROJ-1 -X PUT -d '{"fields":{"summary":"New"}}'
atlassian-cli confluence api /rest/api/content/12345 -q expand=body.storage
```

`--data` takes JSON, `@FILE` or `@-` (stdin); the method defaults to `POST`
when data is given. The command exits non-zero for HTTP status 400 and above.

## HTML to Markdown Conversion

When using `-o markdown`, HTML content (Jira descriptions, Confluence page bodies and comments) is automatically converted to clean GitHub-flavored Markdown. Confluence storage format macros are also handled:

- Code blocks (`ac:structured-macro name="code"`) -> fenced code blocks
- Admonitions (note, info, warning, tip) -> blockquotes with labels
- Page links (`ac:link`) -> emphasized text
- Table of contents macros -> removed

## MCP Server

Start as an MCP server for AI assistant integration:

```bash
atlassian-cli mcp-server
```

### Tool Group Filtering

Filter available tools using the `--tools` flag:

```bash
# Only enable Jira issue and search tools
atlassian-cli mcp-server --tools jira_issue,jira_search

# Only enable Confluence tools
atlassian-cli mcp-server --tools confluence_page,confluence_label
```

Available tool groups:
- `jira_user` - User authentication, user search
- `jira_issue` - Get issue, subtasks
- `jira_search` - Search issues via JQL
- `jira_create` - Create and clone issues (Markdown description)
- `jira_update` - Update and assign issues
- `jira_delete` - Delete issues
- `jira_transition` - Transition issues, get available transitions
- `jira_comment` - List and add comments (Markdown)
- `jira_link` - Link/unlink issues, add web links, list link types
- `jira_worklog` - Log time
- `jira_fields` - List fields and their IDs
- `jira_development` - Pull requests and branches linked to an issue
- `jira_feature` - Issues linked to a feature (by the feature link types)
- `jira_attachment` - List, get, download, upload and delete issue attachments
- `jira_api` - Raw request to any Jira REST endpoint *(opt-in)*
- `confluence_page` - Get, create, update, copy and move pages (Markdown bodies)
- `confluence_search` - Search pages (CQL or text/space), list child pages
- `confluence_label` - Label management
- `confluence_restriction` - Page restriction management
- `confluence_attachment` - List, download, upload and delete page attachments
- `confluence_api` - Raw request to any Confluence REST endpoint *(opt-in)*
- `bookmarks` - List, add and remove the user's bookmarks

Without `--tools`, every group except the *opt-in* ones is enabled; opt-in
groups are only loaded when named.

Tool results are **compact** by default: Markdown text, custom fields under
their display names, links/subtasks/latest comments as short references, and
no API self links or avatars. `jira_get_issue` and `jira_search_issues` take
`full: true` to return the raw API objects.

An agent skill describing the CLI is in [`skills/atlassian-cli/SKILL.md`](skills/atlassian-cli/SKILL.md).

### Claude Code Integration

Add to your Claude Code MCP settings:

```json
{
  "mcpServers": {
    "atlassian": {
      "command": "atlassian-cli",
      "args": ["mcp-server"],
      "env": {
        "JIRA_URL": "https://your-domain.atlassian.net",
        "JIRA_EMAIL": "you@example.com",
        "JIRA_API_TOKEN": "your-api-token",
        "CONFLUENCE_URL": "https://your-domain.atlassian.net/wiki",
        "CONFLUENCE_EMAIL": "you@example.com",
        "CONFLUENCE_API_TOKEN": "your-api-token"
      }
    }
  }
}
```

## Development

```bash
go build -o atlassian-cli .
go test --shuffle on --race ./...
golangci-lint run
```

`tools/devserver` is a local Jira/Confluence server for trying the CLI and the
TUIs without a real instance. It serves exported issues (one directory per
issue with `<KEY>.json` and its attachments, or one JSON array file) over the
REST endpoints the CLI uses, plus a demo Confluence space with pages and
images, a Scrum board with sprints, and epics:

```bash
go run ./tools/devserver -data ./my-issues      # listens on 127.0.0.1:18080
export JIRA_URL=http://127.0.0.1:18080 JIRA_PERSONAL_TOKEN=x
export CONFLUENCE_URL=http://127.0.0.1:18080 CONFLUENCE_PERSONAL_TOKEN=x
./atlassian-cli jira issue list -p ""
./atlassian-cli confluence browse DEMO
```

An optional `names.json` in the data directory maps custom field IDs to names.
Changes are kept in memory only. Never commit exported data from a real
instance.

Releases: pushing a `v*` tag builds the binaries with GoReleaser
(`.github/workflows/release.yml`) and publishes them as a GitHub release.

## Why not the official ACLI?

Atlassian provides an official CLI ([ACLI](https://developer.atlassian.com/cloud/acli/guides/introduction/)) and a [remote MCP server](https://developer.atlassian.com/cloud/acli/guides/introduction/) for AI integration. Here's how this tool differs:

### vs. ACLI

| | atlassian-cli | ACLI |
|---|---|---|
| **Distribution** | Single binary, no dependencies | Requires package installation |
| **Authentication** | Environment variables only | Interactive `acli auth login` |
| **CI/CD friendliness** | High — no browser flow needed | Limited — headless login is cumbersome |
| **Server/DC support** | Yes (PAT auth) | Cloud-focused |
| **Target audience** | Developers, automation, AI agents | Admins, bulk operations |

### vs. Atlassian remote MCP server

Atlassian's remote MCP server went GA in February 2026, but comes with trade-offs:

- **Context-heavy** — loads 73 tool schemas upfront, consuming 40–50% of the context window before any real work
- **OAuth required** — browser-based auth flow; not suitable for headless or CI/CD environments
- **Network dependency** — requires an outbound connection to Atlassian's remote server

This tool's `--tools` flag lets you load only the groups you need, keeping token usage minimal for AI agents.

### When to choose this tool

- Running in **CI/CD pipelines** or automation scripts (auth via environment variables only)
- Using both **Server/Data Center and Cloud** with a single interface
- Embedding as an **MCP server** in AI agents where context efficiency matters

## License

MIT
