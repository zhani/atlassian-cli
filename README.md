# atlassian-cli

<div align="center">
  <img src="docs/images/external/logo/github-banner.png" alt="Logo">
</div>

<div align="center">
  <em>A lightweight, native Go CLI for Atlassian Jira and Confluence</em>
</div>

<br />

<div align="center">
  <a href="https://github.com/putcho01/atlassian-cli/releases">
    <img src="https://img.shields.io/github/v/release/putcho01/atlassian-cli?color=3670A0" alt="Release">
  </a>
  <a href="https://github.com/putcho01/atlassian-cli/commits/main">
    <img src="https://img.shields.io/github/commit-activity/m/putcho01/atlassian-cli" alt="Commit activity">
  </a>
  <a href="https://opensource.org/licenses/MIT">
    <img src="https://img.shields.io/badge/License-MIT-yellow.svg?color=3670A0" alt="License: MIT">
  </a>
  <a href="https://pkg.go.dev/github.com/putcho01/atlassian-cli">
    <img src="https://pkg.go.dev/badge/github.com/putcho01/atlassian-cli.svg" alt="Go Reference">
  </a>
</div>

<br />

## Installation

### go install (recommended)

Requires Go 1.21+:

```bash
go install github.com/putcho01/atlassian-cli@latest
```

> If `$GOPATH/bin` is not in your `$PATH`, add the following to your shell config (`~/.zshrc`, etc.):
>
> ```bash
> export PATH="$PATH:$(go env GOPATH)/bin"
> ```

### From source

```bash
git clone https://github.com/putcho01/atlassian-cli.git
cd atlassian-cli
go build -o atlassian-cli .
```

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
| `JIRA_DEFAULT_PROJECT` | Default project key used when `--project` is omitted (optional) |
| `JIRA_DEFAULT_BOARD` | Agile board ID for `jira sprint` (optional) |
| `JIRA_FEATURE_TYPE` | Issue type of features for `jira feature` (default `New Feature`) |
| `JIRA_FEATURE_LINK_TYPES` | Comma-separated link phrases from a feature to its issues (default `is implemented by,gets values from`) |
| `JIRA_EPIC_LINK_FIELD` | JQL field linking issues to epics (default `parent` on Cloud, `Epic Link` on Server/DC) |
| `JIRA_DETAIL_FIELDS` | Comma-separated custom fields (names or IDs) shown in the TUI issue view, in this order (default: all non-empty) |
| `CONFLUENCE_URL` | Confluence Cloud URL (e.g. `https://your-domain.atlassian.net/wiki`) |
| `CONFLUENCE_EMAIL` | Your Atlassian account email address |
| `CONFLUENCE_API_TOKEN` | API Token |

### Server/Data Center - Personal Access Token (Bearer)

Used with self-hosted Jira/Confluence Server/DC. Authenticates via Bearer auth using a PAT.

| Variable | Description |
|----------|-------------|
| `JIRA_URL` | Jira base URL (e.g. `https://jira.example.com`) |
| `JIRA_PERSONAL_TOKEN` | Personal Access Token |
| `JIRA_DEFAULT_PROJECT` | Default project key used when `--project` is omitted (optional) |
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

## Commands

### Jira

```bash
# Authentication
atlassian-cli jira myself                # Show authenticated user

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
issue browser keys). `tab` switches panes; in the issue pane `l` opens the
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

`jira issue search` opens an interactive browser when run in a terminal without
an explicit `-o`/`--output` (use `--plain` to get the plain table, or `-i` to
force the browser). Piped output is never interactive.

| Key | Action |
|-----|--------|
| `↑`/`↓`, `j`/`k` | Move |
| `g` / `G` | Top / bottom |
| `ctrl+f` / `ctrl+b` | Page down / up |
| `enter` / `v` | View issue (description, custom fields, links, subtasks, latest comments) |
| `esc` | Back / clear filter |
| `/` | Filter the loaded issues |
| `m` | Transition (move) the issue |
| `o` | Open in browser |
| `c` / `ctrl+k` | Copy issue URL / key to the clipboard |
| `ctrl+r` / `F5` | Refresh |
| `?` | Toggle full help |
| `q` | Quit |

The issue view shows custom fields by their display names: longer text fields
(e.g. reproduction steps, comment logs) and checklists as their own sections,
short values in a Fields list. Set `JIRA_DETAIL_FIELDS` to show only selected
fields, e.g. `JIRA_DETAIL_FIELDS="Steps to Reproduce,Comment log"`.

Copying uses `pbcopy`, `xclip`, `xsel` or `wl-copy` when available and falls
back to the OSC 52 terminal escape sequence otherwise. Set `GLAMOUR_STYLE`
(e.g. `dark`, `light`, `dracula`) to override the issue view's colour theme.

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

# Browse interactively: page tree on the left, rendered page on the right
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
