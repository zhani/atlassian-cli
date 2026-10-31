package confluence

import "testing"

func TestBuildCQL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		opts SearchOptions
		want string
	}{
		{SearchOptions{Text: "release notes"}, `type = page AND text ~ "release notes"`},
		{SearchOptions{Text: `say "hi"`, Space: "DEV"}, `type = page AND space = "DEV" AND text ~ "say \"hi\""`},
		{SearchOptions{Space: "DEV", Type: "blogpost"}, `type = "blogpost" AND space = "DEV" ORDER BY lastmodified DESC`},
		{SearchOptions{Space: "DEV", Type: "all"}, `space = "DEV" ORDER BY lastmodified DESC`},
	}
	for _, tt := range tests {
		if got := BuildCQL(tt.opts); got != tt.want {
			t.Errorf("BuildCQL(%+v) =\n  %s\nwant\n  %s", tt.opts, got, tt.want)
		}
	}
}
