package formatter

import (
	"fmt"
	"text/tabwriter"

	"github.com/putcho01/atlassian-cli/internal/confluence"
)

// PrintPageList prints Confluence pages (search results, children, space
// pages) without their bodies.
func (f *Formatter) PrintPageList(pages []confluence.Page) error {
	switch f.format {
	case JSON:
		return f.PrintJSON(pages)
	case Markdown:
		fmt.Fprintf(f.writer, "| ID | Title | Space | Version | Updated |\n|---|---|---|---|---|\n")
		for _, p := range pages {
			fmt.Fprintf(f.writer, "| %s | %s | %s | %d | %s |\n", p.ID, escapePipes(p.Title), spaceKey(p.Space), p.Version.Number, p.Version.When)
		}
		return nil
	default:
		tw := tabwriter.NewWriter(f.writer, 0, 4, 2, ' ', 0)
		fmt.Fprintf(tw, "ID\tTITLE\tSPACE\tVERSION\tUPDATED\n")
		for _, p := range pages {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\n", p.ID, p.Title, spaceKey(p.Space), p.Version.Number, p.Version.When)
		}
		return tw.Flush()
	}
}

// PrintSpaces prints Confluence spaces.
func (f *Formatter) PrintSpaces(spaces []confluence.Space) error {
	switch f.format {
	case JSON:
		return f.PrintJSON(spaces)
	case Markdown:
		fmt.Fprintf(f.writer, "| Key | Name | Type |\n|---|---|---|\n")
		for _, s := range spaces {
			fmt.Fprintf(f.writer, "| %s | %s | %s |\n", s.Key, escapePipes(s.Name), s.Type)
		}
		return nil
	default:
		tw := tabwriter.NewWriter(f.writer, 0, 4, 2, ' ', 0)
		fmt.Fprintf(tw, "KEY\tNAME\tTYPE\n")
		for _, s := range spaces {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", s.Key, s.Name, s.Type)
		}
		return tw.Flush()
	}
}

// PrintVersions prints a page's version history.
func (f *Formatter) PrintVersions(versions []confluence.Version) error {
	switch f.format {
	case JSON:
		return f.PrintJSON(versions)
	case Markdown:
		fmt.Fprintf(f.writer, "| Version | When | By | Message |\n|---|---|---|---|\n")
		for _, v := range versions {
			fmt.Fprintf(f.writer, "| %d | %s | %s | %s |\n", v.Number, v.When, versionAuthor(v), escapePipes(v.Message))
		}
		return nil
	default:
		tw := tabwriter.NewWriter(f.writer, 0, 4, 2, ' ', 0)
		fmt.Fprintf(tw, "VERSION\tWHEN\tBY\tMESSAGE\n")
		for _, v := range versions {
			fmt.Fprintf(tw, "%d\t%s\t%s\t%s\n", v.Number, v.When, versionAuthor(v), v.Message)
		}
		return tw.Flush()
	}
}

func spaceKey(s *confluence.Space) string {
	if s == nil {
		return ""
	}
	return s.Key
}

func versionAuthor(v confluence.Version) string {
	if v.By == nil {
		return ""
	}
	return v.By.DisplayName
}
