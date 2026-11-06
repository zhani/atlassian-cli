package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zhani/atlassian-cli/internal/bookmarks"
	"github.com/zhani/atlassian-cli/internal/config"
	"github.com/zhani/atlassian-cli/internal/confluence"
	"github.com/zhani/atlassian-cli/internal/embeds"
	"github.com/zhani/atlassian-cli/internal/jira"
	"github.com/zhani/atlassian-cli/internal/mirror"
	"github.com/zhani/atlassian-cli/internal/mirrorsync"
)

var mirrorCmd = &cobra.Command{
	Use:   "mirror",
	Short: "Offline mirror of Jira boards, bookmarks and Confluence pages",
	Long: `Copy Jira issues (boards, queries, bookmarks) and Confluence pages with all
their attachments into an archive on a connected machine, and use it on a
machine without access to Jira or Confluence.

  connected machine:  atlassian-cli mirror export --out mirror.tar.gz
  offline machine:    atlassian-cli mirror import mirror.tar.gz

On the offline machine every read-only command, the TUIs and the MCP server
then work from the mirror: when ATLASSIAN_MIRROR names the mirror directory,
or when no JIRA_URL/CONFLUENCE_URL/ATLASSIAN_URL is set and the default
directory (~/.local/share/atlassian-cli/mirror) holds one. Changes are
refused. Re-run export + import to refresh: each issue and page in the
archive replaces its old copy; others are kept (--prune removes them).`,
}

var mirrorExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export boards, queries, bookmarks and pages into a mirror archive",
	Long: `Export into a .tar.gz mirror archive: the issues of each board (--board,
else JIRA_DEFAULT_BOARD) and of each --jql query, the pages of each --page
with all their descendants, and everything bookmarked (issues, queries,
features with their linked issues, epics with their issues, sprints, pages
with their descendants) unless --no-bookmarks. Also, unless turned off: the
issues of the active sprints (of those boards, else of JIRA_DEFAULT_PROJECT;
there may be none, --no-active-sprint) and your issues that are unresolved or
resolved as anything but Done/Fixed (--no-my-issues). The issues one step away
from the exported ones come along (parent, subtasks, epic, every linked issue in
either direction, Xray test coverage), but not their own relatives. Links
across the two sides come along one step too: the Confluence pages those
issues link to (remote links, page links in any field) and the Jira issues
the exported pages show (Jira issue macro) or link to, but nothing these
bring in turn. The issue lists on those pages (Jira issues macros with a
query) run against Jira: their issues come along (up to each list's limit, at
most 1000) and the page keeps the results, so the list shows offline as Jira
answered it. Every issue and page carries its attachments.

Without --since the archive is a full snapshot; with it, only issues and
pages changed since then.

--only jira|confluence exports one side (bookmarks of that kind only;
the bookmark file always comes along). Jira changes often and Confluence
rarely, so export them on their own schedules: importing one side leaves
the other side of the mirror as it is (and --prune only prunes that side),
and links between the two keep resolving because both live in the same
mirror.

Documents a page embeds from elsewhere (a Widget Connector showing a Google
Slides deck, say) are out of reach offline. The embeds file maps their URLs
to local copies, which the export attaches to the page and shows in place of
the embed: a PDF as a slide show (the viewpdf macro; the page browser renders
its pages with mutool or pdftoppm), an image as an image, anything else as a
link. A widget with no mapping is reported. The file is a YAML list:

  - url: https://docs.google.com/presentation/d/1jwmHV7I7qb4viFEZ1Cc2j  # or just the ID
    file: ~/docs/Feature introduction.pdf

A page exported with --since is only re-read when it changed, so after adding
a mapping export without --since (or with --page) once.`,
	Example: `  atlassian-cli mirror export --out mirror.tar.gz
  atlassian-cli mirror export --out mirror.tar.gz --board 42 --jql "project = PROJ AND sprint in openSprints()"
  atlassian-cli mirror export --out delta.tar.gz --since -7d
  atlassian-cli mirror export --out jira.tar.gz --only jira --since -2d
  atlassian-cli mirror export --out wiki.tar.gz --only confluence --since -14d`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		mirror.Disabled = true // export reads the real servers
		out, _ := cmd.Flags().GetString("out")
		boards, _ := cmd.Flags().GetIntSlice("board")
		jqls, _ := cmd.Flags().GetStringArray("jql")
		pages, _ := cmd.Flags().GetStringArray("page")
		noBookmarks, _ := cmd.Flags().GetBool("no-bookmarks")
		noSprint, _ := cmd.Flags().GetBool("no-active-sprint")
		noMine, _ := cmd.Flags().GetBool("no-my-issues")
		since, _ := cmd.Flags().GetString("since")
		only, _ := cmd.Flags().GetString("only")
		embedsPath, _ := cmd.Flags().GetString("embeds")
		required := embedsPath != ""
		if !required {
			embedsPath, _ = embeds.DefaultPath()
		}
		var embedList []embeds.Embed
		if embedsPath != "" {
			var err error
			if embedList, err = embeds.Load(embedsPath, required); err != nil {
				return fmt.Errorf("embeds: %w", err)
			}
		}

		e := &mirrorsync.Exporter{}
		var jerr, cerr error
		if e.JiraCfg, jerr = config.LoadJiraConfig(); jerr == nil {
			e.Jira = jira.NewClient(e.JiraCfg)
		}
		if e.ConfCfg, cerr = config.LoadConfluenceConfig(); cerr == nil {
			e.Confluence = confluence.NewClient(e.ConfCfg)
		}
		if e.Jira == nil && e.Confluence == nil {
			return fmt.Errorf("neither Jira nor Confluence is configured: %w", errors.Join(jerr, cerr))
		}
		_, err := e.Export(cmd.Context(), mirrorsync.ExportOptions{
			Out: out, Boards: boards, JQL: jqls, Pages: pages, Bookmarks: !noBookmarks, Since: since, Only: only, Embeds: embedList, Log: os.Stderr,
			ActiveSprints: !noSprint, MyIssues: !noMine,
		})
		return err
	},
}

func mirrorDirFlag(cmd *cobra.Command) string {
	if d, _ := cmd.Flags().GetString("dir"); d != "" {
		return d
	}
	return mirror.DefaultDir()
}

var mirrorImportCmd = &cobra.Command{
	Use:   "import <archive.tar.gz>",
	Short: "Import a mirror archive into the mirror directory",
	Long: `Import a mirror archive into the mirror directory (--dir, else
ATLASSIAN_MIRROR, else ~/.local/share/atlassian-cli/mirror). Each issue,
page, board and space in the archive replaces its old copy as a whole
(attachments included); everything else is kept. --prune (full snapshots
only) removes the issues and pages the snapshot no longer has, listing them
first; an archive of one side (export --only) prunes only that side, and
leaves the other side's URL and freshness as they were. The local/ subdirectory, for your own additions in the same layout,
is never touched. The archive's bookmarks are added to your bookmarks;
yours are never removed.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		prune, _ := cmd.Flags().GetBool("prune")
		store, err := bookmarks.Open()
		if err != nil {
			return err
		}
		res, err := mirrorsync.Import(args[0], mirrorDirFlag(cmd), mirrorsync.ImportOptions{
			Prune: prune, Bookmarks: store, Log: os.Stderr,
		})
		if err != nil {
			return err
		}
		if isJSONOutput(cmd) {
			return writeJSONOut(res)
		}
		kind := strings.Join(res.Contents, "+") + " full snapshot"
		if !res.Full {
			kind = strings.Join(res.Contents, "+") + " changes since " + res.Manifest.Since
		}
		fmt.Printf("imported %s exported %s into %s\n", kind, res.Manifest.ExportedAt, res.Dir)
		fmt.Printf("  %d new, %d updated, %d kept (not in the archive), %d pruned; %d bookmark(s) added\n",
			res.New, res.Updated, res.Kept, len(res.Pruned), res.Bookmarks)
		m := res.Manifest
		fmt.Printf("  mirror: %d issues, %d pages, %d attachments, exported_at %s\n", m.Issues, m.Pages, m.Attachments, m.ExportedAt)
		return nil
	},
}

func writeJSONOut(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// mirrorStatus is the `mirror status` output.
type mirrorStatus struct {
	Dir           string `json:"dir"`
	Active        bool   `json:"active"`
	ExportedAt    string `json:"exported_at"`
	LastFullAt    string `json:"last_full_at"`
	ImportedAt    string `json:"imported_at"`
	Since         string `json:"since"`
	JiraURL       string `json:"jira_url"`
	ConfluenceURL string `json:"confluence_url"`
	Issues        int    `json:"issues"`
	Pages         int    `json:"pages"`
	Attachments   int    `json:"attachments"`
	Bytes         int64  `json:"bytes"`
	// Per side; nil when that side was never imported.
	Jira       *mirror.SideState `json:"jira"`
	Confluence *mirror.SideState `json:"confluence"`
}

var mirrorStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the mirror directory and how current it is",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		st := mirrorStatus{Dir: mirrorDirFlag(cmd)}
		if active, ok := config.MirrorDir(); ok && active == st.Dir {
			st.Active = true
		}
		if m, err := mirror.ReadManifest(st.Dir); err == nil {
			st.ExportedAt, st.LastFullAt, st.ImportedAt, st.Since = m.ExportedAt, m.LastFullAt, m.ImportedAt, m.Since
			st.JiraURL, st.ConfluenceURL = m.JiraURL, m.ConfluenceURL
			st.Issues, st.Pages, st.Attachments, st.Bytes = m.Issues, m.Pages, m.Attachments, m.Bytes
			st.Jira, st.Confluence = m.Side(mirror.SideJira), m.Side(mirror.SideConfluence)
		}
		if isJSONOutput(cmd) {
			return writeJSONOut(st)
		}
		if st.ExportedAt == "" {
			fmt.Printf("no mirror in %s\n", st.Dir)
			return nil
		}
		fmt.Printf("mirror:       %s (%s)\n", st.Dir, map[bool]string{true: "in use", false: "not in use: set ATLASSIAN_MIRROR or unset the *_URL variables"}[st.Active])
		fmt.Printf("exported at:  %s (last full snapshot %s)\n", st.ExportedAt, st.LastFullAt)
		for _, side := range []struct {
			name string
			st   *mirror.SideState
		}{{"jira", st.Jira}, {"confluence", st.Confluence}} {
			if side.st == nil {
				fmt.Printf("%-13s never imported\n", side.name+":")
				continue
			}
			last := side.st.LastFullAt
			if last == "" {
				last = "none"
			}
			fmt.Printf("%-13s exported %s (last full snapshot %s)\n", side.name+":", side.st.ExportedAt, last)
		}
		fmt.Printf("imported at:  %s\n", st.ImportedAt)
		fmt.Printf("from:         %s %s\n", st.JiraURL, st.ConfluenceURL)
		fmt.Printf("contents:     %d issues, %d pages, %d attachments, %s bytes\n", st.Issues, st.Pages, st.Attachments, strconv.FormatInt(st.Bytes, 10))
		return nil
	},
}

var mirrorServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Serve the mirror over HTTP (Jira at /, Confluence at /wiki)",
	Long: `Serve the mirror read-only over HTTP for other tools: point JIRA_URL at
http://<addr> and CONFLUENCE_URL at http://<addr>/wiki (any token).
atlassian-cli itself does not need this; it reads the mirror in-process.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		addr, _ := cmd.Flags().GetString("addr")
		s, err := mirror.Load(mirrorDirFlag(cmd))
		if err != nil {
			return err
		}
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return err
		}
		base := "http://" + ln.Addr().String()
		s.ServeAt(base)
		fmt.Fprintf(os.Stderr, "serving %s (exported %s): JIRA_URL=%s CONFLUENCE_URL=%s/wiki\n", s, s.Manifest.ExportedAt, base, base)
		return http.Serve(ln, s)
	},
}

func init() {
	rootCmd.AddCommand(mirrorCmd)
	mirrorCmd.AddCommand(mirrorExportCmd, mirrorImportCmd, mirrorStatusCmd, mirrorServeCmd)

	f := mirrorExportCmd.Flags()
	f.String("out", "", "Archive to write (.tar.gz)")
	_ = mirrorExportCmd.MarkFlagRequired("out")
	f.IntSlice("board", nil, "Agile board ID (repeatable; default JIRA_DEFAULT_BOARD)")
	f.StringArray("jql", nil, "Also export the issues of this JQL query (repeatable)")
	f.StringArray("page", nil, "Also export this Confluence page and its descendants (repeatable)")
	f.Bool("no-bookmarks", false, "Do not export the bookmarked items or the bookmark file")
	f.Bool("no-active-sprint", false, "Do not export the issues of the active sprints (of the boards, else of JIRA_DEFAULT_PROJECT)")
	f.Bool("no-my-issues", false, "Do not export your issues that are unresolved or resolved as anything but Done/Fixed")
	f.String("since", "", "Only issues and pages changed since: RFC 3339, 2006-01-02 or a period (-7d, 12h)")
	f.String("only", "", "Export only one side: jira or confluence (default: both)")
	f.String("embeds", "", "File mapping embedded document URLs to local copies (default ATLASSIAN_EMBEDS or ~/.config/atlassian-cli/embeds.yaml)")

	for _, c := range []*cobra.Command{mirrorImportCmd, mirrorStatusCmd, mirrorServeCmd} {
		c.Flags().String("dir", "", "Mirror directory (default ATLASSIAN_MIRROR or ~/.local/share/atlassian-cli/mirror)")
	}
	mirrorImportCmd.Flags().Bool("prune", false, "Remove issues and pages missing from a full snapshot (never local/)")
	mirrorServeCmd.Flags().String("addr", "127.0.0.1:18081", "Listen address")
}
