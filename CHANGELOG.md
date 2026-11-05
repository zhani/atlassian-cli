# Changelog

## [v0.4.6](https://github.com/zhani/atlassian-cli/commits/v0.4.6) - 2026-11-05

### Confluence page browser
- Jira issue lists on pages (the Jira issues macro with a query, a filter) show as a table of the issues they find, with the macro's columns and limit, as in the browser, when Jira is configured. Rich-text columns (description, environment, text-area custom fields) show formatted, as Jira renders them: alone when they are the only column (a `key = X` list showing an issue's description shows just that description), else after the table under each issue's key. `L` also offers the listed issues.

### Offline mirror
- `mirror export` runs the issue lists on the exported pages against Jira, exports their issues (up to each list's limit, at most 1000) and stores the results with the page (`jira-queries.json`). The mirror answers those queries as Jira did, so the lists show offline even when they use JQL the mirror can't evaluate (`linkedIssues()`, ScriptRunner, Xray functions).

## [v0.4.5](https://github.com/zhani/atlassian-cli/commits/v0.4.5) - 2026-11-05

### Offline mirror
- Links between Jira and Confluence open offline. `mirror export` follows them one step: the Confluence pages the exported issues link to (their remote "wiki page" links, and page links in any field — `viewpage.action?pageId=`, `/spaces/…/pages/N`, `/display/SPACE/Title`, tiny `/x/…` links) and the Jira issues the bookmarked and `--page` pages (with descendants) show in a Jira issue macro or link to. What those bring in is not followed further. This happens whatever `--only` says, so a Jira-only archive also carries the pages its issues link to.
- Issues' remote links are exported and served by the mirror (they were always empty).
- `mirror import --prune` keeps items that the mirror's other items still link to, so a one-side full snapshot doesn't drop the issues or pages the other side's export brought in.
- The manifest records both service URLs whenever both are configured, so links are rewritten to the mirror in either direction.

### Interactive browsers
- `l` in the issue detail view also lists the Confluence pages the issue links to (remote links first, then page links in its text); `enter` opens the page in the page browser, and `q` there returns to the issue.
- `L` in `confluence browse` lists the Jira issues the page shows (Jira issue macros) or links to; `enter` opens one in the issue browser, and `q` there returns to the page.
- The Jira issue macro renders as the issue key (or "Jira issues: `JQL`" for an issue list) instead of its raw parameters.

## [v0.4.4](https://github.com/zhani/atlassian-cli/commits/v0.4.4) - 2026-11-03

### Offline mirror
- `mirror export --only jira|confluence` exports one side, so Jira (changes often) and Confluence (changes rarely) can be refreshed on their own schedules into the same mirror, where links between them keep working. Importing one side leaves the other side's items, URL and freshness alone, and `--prune` prunes only that side.
- `mirror status` shows each side's last export and last full snapshot (`jira` / `confluence` in `-o json`); the top-level `last_full_at` is now the older side's.

### Development
- `tools/fakejira` is now `tools/devserver` (it serves Confluence too): `go run ./tools/devserver -data DIR`.

## [v0.4.3](https://github.com/zhani/atlassian-cli/commits/v0.4.3) - 2026-11-02

### Interactive browsers
- `l` in the issue detail view lists the issues it points to — parent, epic, issue links, subtasks, the Xray tests covering it and keys mentioned in its description and comments — and opens the chosen one; `esc` walks back along the way you came (shown in the title, e.g. `P-2 ← P-1`). A link that can't be opened (e.g. not in the mirror) leaves you where you were.
- `i` in the issue detail view shows the issue's images full screen, like the Confluence page browser: the evidence screenshots of its Xray test runs step by step (titled e.g. "Step 6 (FAIL) evidence"), its manual steps' attachments, then image attachments. Works offline from a mirror.
- The issue detail view draws images in place, like the Confluence page browser: Xray evidence under the step it belongs to, step attachments, images in the description, and image attachments in a new Attachments section (kitty placeholders; coloured blocks in other terminals). Images on other hosts are fetched without credentials.

## [v0.4.2](https://github.com/zhani/atlassian-cli/commits/v0.4.2) - 2026-11-02

### Jira
- `--max-results` of `jira issue list`/`search` now defaults to 500 (was 50) and of the feature/epic/sprint explorers to 500 (was 100), so the issue browser shows a whole board or mirror. `JIRA_MAX_RESULTS` changes that default; a `--max-results` flag still wins.

### Xray tests
- `jira issue get -o markdown` and the issue browser show a **Test Details** section for Xray (Server/DC) tests: the manual steps (Action, Data, Expected Result) with each step's latest result, actual result, comment, defects and evidence, from the test runs the TestRunStatus field names. The fields are recognised by shape, whatever their IDs.
- `jira issue evidence KEY [NAME…] [--dir DIR] [--list]` downloads a test's evidence and step attachments.
- `mirror export` stores those test runs and downloads all their evidence, so all of the above works offline.

## [v0.4.1](https://github.com/zhani/atlassian-cli/commits/v0.4.1) - 2026-11-01

### Offline mirror
- `mirror export` also exports the issues one step away from the exported ones: every linked issue ("relates to", "is blocked by", "clones"…, both directions) the Epic Link and the tests covering a requirement (Xray's Requirement Status / Test Coverage field), besides parents and subtasks. Links of those linked issues are not followed.

## [v0.4.0](https://github.com/zhani/atlassian-cli/commits/v0.4.0) - 2026-11-01

### Offline mirror
- Work with Jira and Confluence on a machine that can't reach them, from a snapshot exported on one that can.
- `mirror export --out FILE.tar.gz` writes one archive with a `mirror.json` manifest. It contains:
  - the issues of boards (`--board`, default `JIRA_DEFAULT_BOARD`) and queries (`--jql`), with board and sprint metadata;
  - Confluence pages and their subtrees (`--page`);
  - everything bookmarked, unless `--no-bookmarks`;
  - the issues of the active sprints, if any (of the boards, else of `JIRA_DEFAULT_PROJECT`), unless `--no-active-sprint`;
  - your issues that are unresolved or resolved as anything but Done/Fixed, unless `--no-my-issues`;
  - every issue and page attachment, including the images pages show.
  - `--since` (`-7d`, a date or RFC 3339) limits it to what changed.
- `mirror import FILE` upserts the archive into the mirror directory (`ATLASSIAN_MIRROR`, default `~/.local/share/atlassian-cli/mirror`):
  - each issue, page, board and space it carries replaces its old copy, attachments included;
  - everything else is kept, as are user additions under `local/` and local bookmarks (the archive's bookmarks are merged in);
  - `--prune` removes what a full snapshot no longer has.
- `mirror status` shows how fresh the mirror is (`-o json` for scripts); `mirror serve` serves it over HTTP.
- With a mirror active, every read command works offline with no token: search and list, the browsers and explorers, `confluence browse` with images, attachments, bookmarks and the MCP server. Writes fail with a read-only error.
  - The mirror is active when `ATLASSIAN_MIRROR` is set, or automatically when no Jira/Confluence URL is configured and the default mirror exists.
- Queries run locally:
  - JQL: `AND`/`OR`/`NOT`, comparisons, `~` text search, relative dates, custom fields by name, sprint, epic and link functions, and `ORDER BY`;
  - CQL: title, text, space, ancestor and labels.

### Connections
- `ATLASSIAN_CA_FILE` adds CA certificates (PEM or DER) to trust for Jira, Confluence and page images, on top of the system ones. On macOS it also accepts private server certificates the system verifier rejects despite a trusted keychain CA.

## [v0.3.0](https://github.com/zhani/atlassian-cli/commits/v0.3.0) - 2026-11-01

First release of the [zhani/atlassian-cli](https://github.com/zhani/atlassian-cli) fork. The module path is now `github.com/zhani/atlassian-cli`, so `go install github.com/zhani/atlassian-cli@latest` installs this fork.

### Interactive browsers
- `jira issue search` opens an issue browser on a terminal (`-i` forces it, `--plain` or `-o` turns it off):
  - table with responsive columns and vim-style navigation;
  - detail view rendered as Markdown: description, links, subtasks, comments, custom fields (`JIRA_DETAIL_FIELDS` picks them) and resolution;
  - keys: `m` transitions, `o` opens in the browser, `c`/`ctrl+k` copy the URL/key, `ctrl+r` refreshes, `?` shows help.
- Two-pane explorers, with items on the left and their issues on the right:
  - `jira feature list`: features (`JIRA_FEATURE_TYPE`) and the issues linked to them (`JIRA_FEATURE_LINK_TYPES`);
  - `jira epic list`;
  - `jira sprint list`.
  - `tab` or `ctrl+h`/`ctrl+l` switch panes.
- `confluence browse`: a page tree with the rendered page beside it.
- Images on Confluence pages are drawn in place in the page browser:
  - kitty Unicode placeholders, iTerm2 (WezTerm), Sixel (foot, tmux 3.4+) or coloured half blocks;
  - works inside tmux through passthrough;
  - `i` opens the images full screen;
  - `ATLASSIAN_IMAGES` overrides the detected protocol.
- `/` opens a live fzf-style fuzzy filter in the issue browser, the page tree and the bookmark browser:
  - `↓`/`↑` or `ctrl+j`/`ctrl+k` move between matches;
  - `enter` opens the match;
  - `esc` goes back.
  - The page tree filter loads every page of the spaces you are browsing.

### Bookmarks
- Bookmark issues, queries, features, epics, sprints and Confluence pages. They are stored in `~/.config/atlassian-cli/bookmarks.yaml`, or in the file `ATLASSIAN_BOOKMARKS` points to.
- `bookmark` opens a bookmark browser. Subcommands: `bookmark add|list|open|rename|remove|edit|path`.
- In the browsers:
  - `b` bookmarks the selected item;
  - `S` bookmarks the current query;
  - `B` opens a bookmark picker inside the browser.
- MCP group `bookmarks`.

### Jira
- `jira issue list` builds JQL from filter flags:
  - `-p -s -a -r -t -y -l -w -R`, `--created`/`--updated`, `--order-by`/`--reverse`, `-q`;
  - `~` negates a value; `me` and `x` stand for the current user and unassigned.
- Descriptions and comments are written in Markdown and converted to wiki markup; `--raw` sends the text unchanged.
- Long text can come from the flag, `--template FILE|-`, `-e`/`$EDITOR` or piped stdin.
- New commands:
  - `issue assign`;
  - `issue link`/`unlink`/`link remote`/`link types`;
  - `issue clone`, `issue worklog add`, `issue delete --cascade`;
  - `issue development`;
  - `issue attachment list/get/download/upload/delete`;
  - `field list`, `user search`;
  - `epic add/remove`, `sprint add`.
- `issue get --fields` and `--columns` accept field display names.
- Custom fields are kept when issues are read.
- Search pages through all results: `/search/jql` on Cloud, `startAt` on Server/DC.
- Issue lists support `-o csv`, `--columns` and `--no-headers`.
- `jira api` sends authenticated requests to any REST path.

### Confluence
- `confluence search` (CQL, or `--text`/`--space`/`--type`), `space list`, and `page list`/`children`/`versions`.
- Page bodies are written in Markdown and converted to storage format (code and admonition macros, images); `--raw` sends storage XHTML.
- `page edit` opens the page as Markdown in `$EDITOR`. It refuses pages whose features Markdown would lose, unless you pass `--raw` or `--allow-lossy`.
- `page get --content-only`, `page copy`, `page move`.
- `attachment list/download/upload/delete`.
- `confluence api` sends raw requests.
- Fixes: empty code blocks and multi-paragraph notes in Markdown output.

### MCP server
- New groups:
  - `jira_comment`, `jira_attachment`, `jira_link`, `jira_worklog`, `jira_fields`, `jira_development`, `jira_feature`;
  - `confluence_search`, `confluence_attachment`;
  - `bookmarks`.
- `confluence_page` can create and update pages with Markdown bodies.
- `jira_api` and `confluence_api` are opt-in groups for raw requests.
- Results are compact by default; `full: true` returns the raw API objects.
- Fix: the server panicked on startup with go-sdk v1.5.0.

### Other
- `ATLASSIAN_URL`/`ATLASSIAN_EMAIL`/`ATLASSIAN_API_TOKEN`/`ATLASSIAN_PERSONAL_TOKEN` work as shared credentials for both Jira and Confluence.
- Runtime errors no longer print the command usage.
- Release binaries for Linux, macOS and Windows (amd64/arm64) are built from version tags. They are statically linked, with the version taken from the tag.
- `tools/fakejira` is a local dev server that serves exported Jira data and a demo Confluence, so you can try the CLI without an Atlassian instance.
- Known issue: the full-screen image preview stays blank in WezTerm on Windows inside tmux over SSH (see `TODO.md`).

Releases before v0.3.0 were made in the upstream repository [putcho01/atlassian-cli](https://github.com/putcho01/atlassian-cli).

## [v0.2.6](https://github.com/putcho01/atlassian-cli/compare/v0.2.5...v0.2.6) - 2026-05-11
- :wrench:(release): use existing draft release in GoReleaser by @putcho01 in https://github.com/putcho01/atlassian-cli/pull/12

## [v0.2.5](https://github.com/putcho01/atlassian-cli/compare/v0.2.4...v0.2.5) - 2026-05-11
- fix tagpr.yml by @putcho01 in https://github.com/putcho01/atlassian-cli/pull/10

## [v0.2.4](https://github.com/putcho01/atlassian-cli/compare/v0.2.3...v0.2.4) - 2026-05-11
- :wrench:(release): set up automated release flow with tagpr by @putcho01 in https://github.com/putcho01/atlassian-cli/pull/8

## [v0.2.3](https://github.com/putcho01/atlassian-cli/compare/0.2.1...v0.2.3) - 2026-05-09
- fix(htmlconv): fix heading text loss and normalize Confluence HTML output by @putcho01 in https://github.com/putcho01/atlassian-cli/pull/7

## [v0.2.2](https://github.com/putcho01/atlassian-cli/compare/0.2.0...v0.2.2) - 2026-05-09
- chore(release): add GoReleaser config and release workflow by @putcho01 in https://github.com/putcho01/atlassian-cli/pull/6

## [0.2.1](https://github.com/putcho01/atlassian-cli/compare/0.2.0...0.2.1) - 2026-05-09
- chore(release): add GoReleaser config and release workflow by @putcho01 in https://github.com/putcho01/atlassian-cli/pull/6

## [0.2.0](https://github.com/putcho01/atlassian-cli/compare/0.1.0...0.2.0) - 2026-05-09
- feat(jira): add JIRA_DEFAULT_PROJECT env var for default project fallback by @putcho01 in https://github.com/putcho01/atlassian-cli/pull/1
- feat(jira): add "issue open" command to open issue in browser by @putcho01 in https://github.com/putcho01/atlassian-cli/pull/2
- feat(jira): add "issue comment list/add" commands by @putcho01 in https://github.com/putcho01/atlassian-cli/pull/3
- feat(confluence): add "page create" and "page update" commands by @putcho01 in https://github.com/putcho01/atlassian-cli/pull/4
- feat(tui): add interactive issue picker with bubbletea by @putcho01 in https://github.com/putcho01/atlassian-cli/pull/5

## [0.1.0](https://github.com/putcho01/atlassian-cli/commits/0.1.0) - 2026-05-08
