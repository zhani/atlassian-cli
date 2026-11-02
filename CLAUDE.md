# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Development

```bash
# Build
go build -o atlassian-cli .

# Run tests
go test --shuffle on --race -v ./...

# Run tests for a specific package
go test ./internal/htmlconv/...

# Run a single test
go test ./internal/htmlconv/ -run TestConvert

# Lint
golangci-lint run
```

## Architecture

The project is a single Go binary (`main.go` → `cmd.Execute()`) structured as a CLI + MCP server.

### Package layout

- **`cmd/`** — Cobra command tree. Each file corresponds to a command group (e.g., `jira_issue_get.go`, `confluence_page.go`). Commands instantiate an API client, call it, and delegate rendering to `internal/formatter`.
- **`internal/config/`** — Reads `JIRA_*` / `CONFLUENCE_*` environment variables, falling back to shared `ATLASSIAN_*` ones (a shared email is only paired with an API token, never a PAT). If `EMAIL` is set, Basic auth (Cloud) is used; otherwise Bearer auth (Server/DC). `LoadJiraConfig()` / `LoadConfluenceConfig()` are the entry points.
- **`internal/httpclient/`** — Thin HTTP client wrapper that injects auth headers. `Transport()` (`tls.go`) is the shared RoundTripper for real servers, also used for page images: the default transport plus `ATLASSIAN_CA_FILE` roots (system pool + file; on macOS Go retries with its own verifier when the platform one fails).
- **`internal/jira/`** — Jira REST API client and type definitions (`types.go`).
- **`internal/confluence/`** — Confluence REST API client and type definitions (`types.go`).
- **`internal/formatter/`** — Renders API responses in three formats: `table` (tab-aligned), `json`, `markdown` (plus `csv` for issue lists; `columns.go` holds the selectable issue columns). HTML content is piped through `htmlconv.Convert()` when using markdown format.
- **`internal/mdconv/`** — Markdown → Jira wiki markup (`ToJiraWiki`, goldmark AST walk) and Markdown → Confluence storage (`ToStorage`, goldmark HTML renderer with custom nodes for code/admonition macros and images; ```` ```storage ```` fences pass through). `ToStorage` is the inverse of `htmlconv.Convert`; `TestStorageRoundTrip` guards that. CLI and MCP convert text with these unless `--raw` / `body_format: storage`.
- **`internal/htmlconv/`** — HTML-to-GFM converter (`Unsupported()` lists storage features a Markdown round trip would lose; `confluence page edit` refuses such pages). Handles standard HTML tags plus Confluence storage format macros (`ac:structured-macro`, `ac:link`, `ac:emoticon`, etc.). This is where macro rendering logic lives.
- **`internal/mcptools/`** — MCP server implementation using `github.com/modelcontextprotocol/go-sdk`. `groups.go` maps group names to tool names (`optInGroups` are only enabled when named); `server.go` creates the server and registers tools via `maybeAdd()`. `compact.go` holds the default compact result shapes (`full: true` returns raw API objects). Every new tool must be added to a group — `TestToolGroups` checks it.
- **`internal/tui/`** — Bubbletea-based interactive issue browser for `jira issue search` (auto on a TTY, `-i` forces, `--plain` disables). The model talks to Jira through the `tui.Source` interface (implemented by `*jira.Client`), so refresh/transition/detail loading happen inside the TUI; tests drive `Update` against a fake source. `explorer.go` wraps that issue model as the right pane of a two-pane explorer (feature/epic/sprint commands in `cmd/jira_explorers.go`; `issuesLoadedMsg.jql` drops stale results when the selection changes); `pagetree.go` is the Confluence page browser (`confluence browse`). `issuefilter.go` (issue list), `pagefilter.go` (page tree) and the bookmark browser share the `/` fuzzy finder (`filterBox`, `highlight`/`matchStyle`; filtered issue lists are drawn by `filteredView`, since bubbles/table cells cannot hold highlights); `pagefilter.go` is the page tree's (`fuzzy.go` matcher; opening it loads whole spaces via `SpaceTreeSource` and merges them into the tree). `pageimages.go` downloads a page's images (attachments via `ImageSource`) and draws them in place by replacing `@@imgN@@` markers after glamour rendering; `i` hands the terminal to `termimg.Preview` through `tea.Exec`. The issue detail view has the same `i` (`issueimages.go`): `issueImageRefs` orders Xray step evidence, run evidence, step attachments, image attachments; downloads go through the source's optional `DownloadAttachment` (`attachmentSource`), decoded images are cached per issue; image support is detected before the program starts (`newGraphics`, in `RunIssueList`/`RunExplorer`, which also run with `termimg.Output`). The detail view also draws images in place (`issueinline.go`): `issueMarkdownWith(…, true)` embeds them (`xray.MarkdownEmbed` after each Evidence line, an Attachments section), then the page browser's pipeline, shared as plain functions in `pageimages.go` (`extractImages` → glamour → `placeImages`, `kittyTransmitCmd`); `issueInline` per loaded issue (pointer identity drops stale results), kitty images deleted on refresh/exit; `fetchInline` sends credentials only to site URLs. `l` (`issuelinks.go`, `modeLinks`) lists `issueLinkItems` — parent (`IssueFields.Parent`), Epic Link (by name), issue links, subtasks, `xray.CoverageTests`, then keys mentioned in description/comments limited to already related projects — and follows one; `detailTrail` makes esc walk back before returning to the list; a failed load of a followed key steps back with a status.
- **`internal/bookmarks/`** — Bookmark store (`bookmarks.yaml` under the user config dir, or `ATLASSIAN_BOOKMARKS`): issues, queries, features, epics, sprints, pages. `Describe` looks items up for title/link (shared by `cmd/bookmark.go` and the `bookmarks` MCP group). TUIs take a `tui.Bookmarker` (`b` toggles, `S` saves a query, `B` opens the bookmark browser embedded as a picker: `newPicker`/`updatePicker`; the page tree reveals a picked page via `revealPage`, loading its space or switching to it; the issue browser resolves feature/epic/sprint bookmarks through `Options.ResolveBookmark`); `tui.RunBookmarks` is the standalone bookmark browser, and `cmd/bookmark.go` loops between it and the opened view.
- **`internal/xray/`** — Xray (Server/DC) test data, recognised by shape since field IDs differ per server: `ManualSteps` (the Manual Test Steps field, two layouts), `RunRefs` (latestId/latestFinalId in TestRunStatus), `Files` (any `fileName`+`fileURL` object: evidence, step attachments), `Markdown` (the Test Details section). `jira.Client.LoadXrayRuns` fills `Issue.XrayRuns` (`/rest/raven/1.0/api/testrun/{id}`, best effort) for `issue get`, the TUI detail view (`xraySource`) and `issue evidence`; the export stores runs as `xray/testrun-<id>.json` + files under `xray/files/` with `xray/urls.json` (site path → file), which the mirror serves at Xray's URLs.
- **`internal/termimg/`** — Terminal images: protocol detection (`Detect`: `ATLASSIAN_IMAGES`, env, tmux client info, XTVERSION/DA1 query), half-block text, kitty Unicode placeholders (`diacritics.go` is generated from kitty's table), iTerm2 and Sixel encoders, tmux passthrough wrapping. `Output` serializes image escape sequences with bubbletea's frames.

- **`internal/mirror/`** — Offline mirror: `Server` loads a mirror directory (layout in `mirror.go`; `local/` overlays it) and serves the REST endpoints the clients use, read-only, as an in-process `http.RoundTripper` (`Transport`). `config.loadConfig` switches to it (`mirrorConfig`: base URLs `JiraBase`/`ConfluenceBase`, explorer settings from the manifest) when `ATLASSIAN_MIRROR` is set or no service URL is set and the default directory has `mirror.json`; `httpclient.NewFromConfig` plugs the transport in for those bases, so clients/TUI/MCP are unchanged. Original server URLs in served JSON are rewritten to the mirror bases. `query.go` parses JQL/CQL (AND/OR/NOT, ORDER BY, functions); `jql.go`/`cql.go` evaluate them. It must not import other internal packages (httpclient imports it).
- **`internal/mirrorsync/`** — `mirror export` (raw REST via `Raw`, downloads attachments, writes the layout, tars it; sources: boards, `--jql`, bookmarks, `--page`, plus by default the active sprints of the boards — `activeSprintJQL`, default-project fallback — and `myIssuesJQL`: own issues not resolved as Done/Fixed, naming only resolutions the server has; then `relatedKeys` adds one non-recursive level of relatives: parent, subtasks, `issuelinks` both ways, Epic Link field, and `xray.CoverageTests`: tests in Xray Requirement Status `…Jql` lists, detected by shape since its field ID differs per server) and `mirror import` (upsert per issue/page/board/space dir via renames from a staging dir next to the mirror, `--prune` for full snapshots, union-merges bookmarks, never touches `local/`).

### Key design patterns

- **Long text input**: `cmd/body.go` — `addBodyFlags` + `readBody` give a flag, `--template FILE|-`, `-e/--editor`; required text also falls back to piped stdin, then `$EDITOR` on a TTY. Optional text never reads stdin implicitly.
- **Issue lists**: `cmd/issue_list_output.go` — `addIssueListFlags` + `showIssues` shared by `issue search` and `issue list` (TUI vs. formatter decision in `useInteractive`). `jira.BuildJQL` turns `issue list` filter flags into JQL.
- **Errors**: `rootCmd.PersistentPreRun` sets `SilenceUsage` once flags/args validated, so runtime errors print without the usage text.
- **Server/DC vs Cloud**: `jira.Client.cloud` (email set or `*.atlassian.net`) selects user references (`accountId` vs `name`, see `userRef`/`UserID`) and the search endpoint. Shared operation logic (user/link-type/field resolution, clone) lives in `internal/jira/operations.go` and is used by both CLI and MCP.
- **Dev server**: `go run ./tools/fakejira -data <dir>` serves exported issues over the REST endpoints the CLI uses (optional `<dir>/names.json` maps custom field IDs to names); never commit real exported data.

- **Output format**: All commands take `--output` / `-o` (table/json/markdown) via a persistent root flag. Commands read it with `cmd.Root().PersistentFlags().GetString("output")`, then call `formatter.ParseFormat()`.
- **Auth dispatch**: `config.loadConfig()` tries `_API_TOKEN` first, then `_PERSONAL_TOKEN`. If `Email` is non-empty in the resulting struct, the HTTP client uses Basic auth; otherwise Bearer.
- **MCP tool filtering**: `mcptools.maybeAdd()` checks `isToolEnabled(name, enabledGroups)` before registering each tool. The `--tools` CLI flag passes a comma-separated list of group names.
- **HTML conversion**: `htmlconv.Convert()` uses `golang.org/x/net/html` to parse, then walks the node tree with `renderElement()`. Confluence-specific tags (`ac:*`, `ri:*`) are handled before standard HTML tags.

## Release

Releases are automated via GitHub Actions (`.github/workflows/release.yml`) triggered on `v*` tags. GoReleaser builds cross-platform binaries for linux/darwin/windows × amd64/arm64.
