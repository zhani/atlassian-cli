package formatter

import (
	"encoding/csv"
	"fmt"
	"strings"
	"text/tabwriter"
)

// PrintRecords prints rows under headers as an aligned table, CSV or
// Markdown table, and v as JSON in JSON mode. It serves the simple list outputs
// that need no format-specific layout.
func (f *Formatter) PrintRecords(v any, headers []string, rows [][]string) error {
	switch f.format {
	case JSON:
		return f.PrintJSON(v)
	case CSV:
		w := csv.NewWriter(f.writer)
		if !f.noHeaders {
			_ = w.Write(headers)
		}
		_ = w.WriteAll(rows) // flushes
		return w.Error()
	case Markdown:
		fmt.Fprintf(f.writer, "| %s |\n|%s\n", strings.Join(headers, " | "), strings.Repeat("---|", len(headers)))
		for _, r := range rows {
			cells := make([]string, len(r))
			for i, c := range r {
				cells[i] = escapePipes(strings.ReplaceAll(c, "\n", " "))
			}
			fmt.Fprintf(f.writer, "| %s |\n", strings.Join(cells, " | "))
		}
		return nil
	default:
		tw := tabwriter.NewWriter(f.writer, 0, 4, 2, ' ', 0)
		if !f.noHeaders {
			upper := make([]string, len(headers))
			for i, h := range headers {
				upper[i] = strings.ToUpper(h)
			}
			fmt.Fprintln(tw, strings.Join(upper, "\t"))
		}
		for _, r := range rows {
			fmt.Fprintln(tw, strings.Join(r, "\t"))
		}
		return tw.Flush()
	}
}
