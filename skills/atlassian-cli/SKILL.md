---
name: atlassian-cli
description: Read and update Jira issues and Confluence pages from the shell with atlassian-cli (Cloud and Server/Data Center). Use when a task involves Jira issues, JQL, comments, transitions, attachments, or Confluence pages, search and edits.
---

# atlassian-cli

`atlassian-cli` talks to Jira and Confluence over their REST APIs. Credentials
come from environment variables (`JIRA_URL` + `JIRA_API_TOKEN`/`JIRA_EMAIL` or
`JIRA_PERSONAL_TOKEN`, the same with `CONFLUENCE_`, or shared `ATLASSIAN_*`).
Check them with `atlassian-cli jira myself`.

## Offline mirror

On a machine without access to Jira/Confluence, the same commands may read
an offline mirror (made elsewhere with `mirror export`, imported with
`mirror import`). Check with `atlassian-cli mirror status -o json`: when it
shows an `exported_at`, the data is a snapshot as of that time and
everything is read-only — find issues, descriptions, comments, attachments
and pages as usual (`jira issue get KEY -o markdown`, `jira issue search JQL
-o json`, `jira issue attachment download KEY FILE`, `jira issue evidence KEY --dir
DIR` (Xray test evidence: the screenshots a tester attached to a step), `confluence search
--text …`, `confluence page get ID -o markdown`, `bookmark list -o json`),
but don't try to comment, transition or edit (it fails with "read-only
mirror"); tell the user what to change instead. Say how current the data is
when it matters. History JQL (`WAS`, `CHANGED`) is not supported there. The
user refreshes it by exporting and importing again.

## Rules for agents

- Xray tests: `jira issue get KEY -o markdown` ends with a **Test Details**
  section — every manual step and, from the latest test run, its status
  (e.g. FAIL), comment, actual result, defects and evidence file names.
  Download the evidence with `jira issue evidence KEY --dir DIR` and look at
  the images before concluding why a step failed.

- Always pass `-o markdown` (readable) or `-o json` (structured). Without `-o`,
  `jira issue search` / `list` and `jira feature` / `epic` / `sprint list`
  open an interactive browser on a terminal. `confluence browse` is
  interactive only.
- Text you send is **Markdown**: descriptions, comments and Confluence page
  bodies are converted to Jira wiki markup / Confluence storage format. Add
  `--raw` only when you already have wiki markup or storage XHTML.
- Long text: use `--template FILE` or `--template -` with stdin. Never rely on
  `$EDITOR` (`-e`, `confluence page edit`) – it needs a human at a terminal.
- `confluence page update` replaces the whole page: fetch it first with
  `page get ID --content-only`, edit, then send the full Markdown back.

## Bookmarks

The user's bookmarks name the issues, queries, features, epics, sprints and
pages they work with; start there when they refer to "my work", a named
query or a bookmark:

```bash
atlassian-cli bookmark list -o json                         # n, kind, key / jql, name, title
atlassian-cli bookmark open "My open work" -o markdown       # a query's issues, an issue, a page…
atlassian-cli bookmark add PROJ-1                            # or: add query "JQL" --name N / feature|epic KEY / sprint ID / page ID
```

## Jira

```bash
atlassian-cli jira issue get PROJ-1 -o markdown
atlassian-cli jira issue search 'project = PROJ AND status = "In Progress"' -o json --max-results 100
atlassian-cli jira issue list -a me -s~Done -o markdown          # filter flags → JQL (see --help)
atlassian-cli jira issue list -t Bug --created -7d --show-jql     # print the JQL only
atlassian-cli jira issue create --project PROJ --type Bug --summary "Crash on start" --template desc.md
atlassian-cli jira issue comment add PROJ-1 --body "Fixed in **v1.2**"
atlassian-cli jira issue transition PROJ-1 "In Progress"
atlassian-cli jira issue assign PROJ-1 me                          # or x, default, a name/email
atlassian-cli jira issue link PROJ-1 PROJ-2 "is blocked by"        # reads: PROJ-1 is blocked by PROJ-2
atlassian-cli jira issue worklog add PROJ-1 "1h 30m" --comment "Review"
atlassian-cli jira issue get PROJ-1 --fields status,"Story Points" -o json
atlassian-cli jira field list --custom "points"                    # find custom field IDs
atlassian-cli jira issue development PROJ-1 -o json                # linked PRs and branches
atlassian-cli jira issue attachment upload PROJ-1 report.pdf
atlassian-cli jira feature list FEAT-12 -o json                    # issues linked to a feature, with the link
atlassian-cli jira epic list EPIC-7 -o json                        # issues of an epic
atlassian-cli jira sprint list --current -o json                   # issues of the active sprint
atlassian-cli jira issue attachment download PROJ-1 log.txt --file -
atlassian-cli jira api /rest/api/2/field                           # any REST endpoint
```

## Confluence

```bash
atlassian-cli confluence search --text "release process" --space DEV -o json
atlassian-cli confluence search 'label = "runbook" AND space = OPS' -o json
atlassian-cli confluence page get 12345 --content-only            # body as Markdown
atlassian-cli confluence page children 12345 -o json
atlassian-cli confluence page create --space DEV --title "Notes" --template notes.md
atlassian-cli confluence page update 12345 --title "Notes" --template notes.md -m "Update"
atlassian-cli confluence page move 12345 678                       # under page 678
atlassian-cli confluence attachment upload 12345 diagram.png
atlassian-cli confluence api /rest/api/space -q limit=10
```

Markdown features with Confluence equivalents: fenced code blocks (code
macro), `> **Note:**` / `Info:` / `Warning:` / `Tip:` quotes (panels),
tables, images (URL or attachment filename). Embed other storage XHTML in a
fenced block with the info string `storage`.

## MCP server

`atlassian-cli mcp-server` exposes the same operations as MCP tools; limit
them with `--tools jira_issue,jira_search,...`. Tool results are compact by
default; pass `full: true` for raw API objects. The raw request tools
(`jira_api`, `confluence_api` groups) are only enabled when named.
