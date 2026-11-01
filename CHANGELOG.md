# Changelog

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
