package termimg

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func pngOf(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// twoTone is a w×h image: red top half, blue bottom half.
func twoTone(t *testing.T, w, h int) *Image {
	t.Helper()
	src := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{255, 0, 0, 255}
			if y >= h/2 {
				c = color.RGBA{0, 0, 255, 255}
			}
			src.Set(x, y, c)
		}
	}
	img, err := Decode(pngOf(t, src))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func TestDecodeFlattensTransparency(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 4, 4)) // fully transparent
	img, err := Decode(pngOf(t, src))
	if err != nil {
		t.Fatal(err)
	}
	if got := img.RGBA.RGBAAt(1, 1); got != (color.RGBA{255, 255, 255, 255}) {
		t.Errorf("transparent pixel = %v, want white", got)
	}
	if len(img.PNG) == 0 {
		t.Error("no PNG encoding")
	}
	if _, err := Decode([]byte("not an image")); err == nil {
		t.Error("Decode accepted garbage")
	}
}

func TestDecodeShrinksHugeImages(t *testing.T) {
	img, err := Decode(pngOf(t, image.NewGray(image.Rect(0, 0, 4000, 1000))))
	if err != nil {
		t.Fatal(err)
	}
	if w, h := img.Size(); w != maxSide || h != 500 {
		t.Errorf("size = %d×%d, want %d×500", w, h, maxSide)
	}
}

func TestFit(t *testing.T) {
	tests := []struct {
		name                                       string
		imgW, imgH, maxCols, maxRows, cellW, cellH int
		wantCols, wantRows                         int
	}{
		{"native size", 80, 32, 100, 100, 8, 16, 10, 2},
		{"width bound", 800, 160, 50, 100, 8, 16, 50, 5},
		{"height bound", 800, 1600, 100, 20, 8, 16, 20, 20},
		{"tiny", 1, 1, 10, 10, 8, 16, 1, 1},
		{"empty", 0, 10, 10, 10, 8, 16, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, r := Fit(tt.imgW, tt.imgH, tt.maxCols, tt.maxRows, tt.cellW, tt.cellH)
			if c != tt.wantCols || r != tt.wantRows {
				t.Errorf("Fit = %d×%d, want %d×%d", c, r, tt.wantCols, tt.wantRows)
			}
		})
	}
}

func TestHalfBlocks(t *testing.T) {
	lines := HalfBlocks(twoTone(t, 4, 2), 4, 1)
	if len(lines) != 1 {
		t.Fatalf("got %d lines", len(lines))
	}
	l := lines[0]
	if !strings.Contains(l, "\x1b[38;2;255;0;0m") || !strings.Contains(l, "\x1b[48;2;0;0;255m") {
		t.Errorf("want red over blue, got %q", l)
	}
	if strings.Count(l, "\x1b[38;2") != 1 {
		t.Errorf("unchanged colors repeated: %q", l)
	}
	if w := ansi.StringWidth(l); w != 4 {
		t.Errorf("width = %d, want 4", w)
	}
}

func TestKittyPlaceholders(t *testing.T) {
	id := uint32(7)<<24 | 42
	lines := KittyPlaceholders(id, 5, 3)
	if len(lines) != 3 {
		t.Fatalf("got %d lines", len(lines))
	}
	for r, l := range lines {
		if !strings.HasPrefix(l, "\x1b[38;5;42m") {
			t.Errorf("line %d does not set color 42: %q", r, l)
		}
		for c := 0; c < 5; c++ {
			cell := string(placeholder) + string(diacritics[r]) + string(diacritics[c]) + string(diacritics[7])
			if !strings.Contains(l, cell) {
				t.Errorf("line %d lacks the diacritics of column %d", r, c)
			}
		}
		if w := ansi.StringWidth(l); w != 5 {
			t.Errorf("line %d width = %d, want 5", r, w)
		}
	}
}

func TestKittyTransmitChunks(t *testing.T) {
	data := bytes.Repeat([]byte{1, 2, 3}, 5000) // 20000 base64 bytes
	seqs := KittyTransmit(9, data, 10, 4)
	if len(seqs) != 5 {
		t.Fatalf("got %d chunks, want 5", len(seqs))
	}
	if !strings.HasPrefix(seqs[0], "\x1b_Ga=T,U=1,f=100,t=d,i=9,p=1,c=10,r=4,q=2,m=1;") {
		t.Errorf("first chunk = %.60q", seqs[0])
	}
	if !strings.HasPrefix(seqs[4], "\x1b_Gm=0,q=2;") || !strings.HasSuffix(seqs[4], "\x1b\\") {
		t.Errorf("last chunk = %.40q", seqs[4])
	}
}

func TestTmuxWrap(t *testing.T) {
	got := TmuxWrap("\x1b_Gq=2\x1b\\")
	want := "\x1bPtmux;\x1b\x1b_Gq=2\x1b\x1b\\\x1b\\"
	if got != want {
		t.Errorf("TmuxWrap = %q, want %q", got, want)
	}
	c := Caps{Tmux: true}
	if c.Wrap("x", true) != "x" {
		t.Error("native sequences must not be wrapped")
	}
	if (Caps{}).Wrap("\x1bx", false) != "\x1bx" {
		t.Error("wrapped outside tmux")
	}
}

func TestSixelImage(t *testing.T) {
	s := SixelImage(twoTone(t, 10, 12), 10, 12)
	if !strings.HasPrefix(s, "\x1bP0;1;0q\"1;1;10;12#0;2;") || !strings.HasSuffix(s, "\x1b\\") {
		t.Errorf("bad framing: %.40q … %q", s, s[len(s)-4:])
	}
	if n := strings.Count(s, "-"); n != 2 {
		t.Errorf("got %d bands, want 2", n)
	}
	// Each band has a uniform color over the full width: one run of 10.
	if !strings.Contains(s, "!10~") {
		t.Errorf("expected a full-width run: %q", s[strings.Index(s, "-")-20:])
	}
}

func TestITermImage(t *testing.T) {
	s := ITermImage([]byte("abc"), 20, 5)
	if s != "\x1b]1337;File=inline=1;size=3;width=20;height=5;preserveAspectRatio=1:YWJj\a" {
		t.Errorf("got %q", s)
	}
}

func TestParseProtocol(t *testing.T) {
	for in, want := range map[string]Protocol{"kitty": Kitty, "ITerm2": ITerm, "sixel": Sixel, "blocks": Blocks, "off": Off} {
		p, ok, err := ParseProtocol(in)
		if err != nil || !ok || p != want {
			t.Errorf("ParseProtocol(%q) = %v, %v, %v", in, p, ok, err)
		}
	}
	if _, ok, err := ParseProtocol("auto"); ok || err != nil {
		t.Error("auto must ask for detection")
	}
	if _, _, err := ParseProtocol("png"); err == nil {
		t.Error("unknown value accepted")
	}
}

func TestDetectForced(t *testing.T) {
	t.Setenv("TMUX", "")
	t.Setenv("ATLASSIAN_IMAGES", "kitty")
	c, err := Detect()
	if err != nil || c.InPage != Kitty || c.Preview != Kitty {
		t.Errorf("kitty: %+v %v", c, err)
	}
	t.Setenv("ATLASSIAN_IMAGES", "iterm")
	if c, _ := Detect(); c.InPage != Blocks || c.Preview != ITerm {
		t.Errorf("iterm: %+v", c)
	}
	t.Setenv("ATLASSIAN_IMAGES", "off")
	if c, _ := Detect(); c.InPage != Off || c.Preview != Off {
		t.Errorf("off: %+v", c)
	}
}

func TestDetectFromEnvironment(t *testing.T) {
	t.Setenv("TMUX", "")
	t.Setenv("ATLASSIAN_IMAGES", "")
	t.Setenv("KITTY_WINDOW_ID", "")
	t.Setenv("TERM_PROGRAM", "")
	for term, want := range map[string]Caps{
		"xterm-kitty": {InPage: Kitty, Preview: Kitty},
		"wezterm":     {InPage: Blocks, Preview: ITerm},
		"foot":        {InPage: Blocks, Preview: Sixel},
	} {
		t.Setenv("TERM", term)
		c, err := Detect()
		if err != nil || c.InPage != want.InPage || c.Preview != want.Preview {
			t.Errorf("TERM=%s: %+v %v", term, c, err)
		}
	}
}

func TestPassthroughFallback(t *testing.T) {
	c := Caps{InPage: Kitty, Preview: Kitty, Tmux: true}.passthroughFallback()
	if c.InPage != Blocks || c.Preview != Blocks || c.Hint() == "" {
		t.Errorf("kitty without passthrough: %+v hint %q", c, c.Hint())
	}
	c = Caps{InPage: Blocks, Preview: Sixel, Tmux: true, TmuxSixel: true}.passthroughFallback()
	if c.Preview != Sixel {
		t.Errorf("tmux Sixel needs no passthrough: %+v", c)
	}
	c = Caps{InPage: Kitty, Preview: Kitty, Tmux: true, Passthrough: true}.passthroughFallback()
	if c.InPage != Kitty || c.Hint() != "" {
		t.Errorf("with passthrough: %+v", c)
	}
}
