package pdfslides

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// twoPagePDF builds a minimal PDF with a landscape and a portrait page.
func twoPagePDF() []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 320 180] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 180 320] >>",
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return b.Bytes()
}

func TestIsPDF(t *testing.T) {
	if !IsPDF([]byte("\n%PDF-1.7 ...")) || IsPDF([]byte("\x89PNG")) || IsPDF(nil) {
		t.Error("IsPDF")
	}
}

func TestRenderWithEachTool(t *testing.T) {
	for _, tool := range []string{"mutool", "pdftoppm"} {
		t.Run(tool, func(t *testing.T) {
			path, err := exec.LookPath(tool)
			if err != nil {
				t.Skip(tool + " not installed")
			}
			only(t, tool, path)
			cache := t.TempDir()
			t.Setenv("ATLASSIAN_SLIDES_CACHE", cache)
			pdf := twoPagePDF()
			pages, err := Render(context.Background(), pdf)
			if err != nil {
				t.Fatal(err)
			}
			if len(pages) != 2 {
				t.Fatalf("got %d pages", len(pages))
			}
			for i, want := range [][2]int{{Size, Size * 180 / 320}, {Size * 180 / 320, Size}} {
				cfg, err := png.DecodeConfig(bytes.NewReader(pages[i]))
				if err != nil {
					t.Fatal(err)
				}
				// Renderers round the scaled side differently.
				if d := cfg.Width - want[0] + cfg.Height - want[1]; d < -2 || d > 2 || max(cfg.Width, cfg.Height) != Size {
					t.Errorf("page %d is %d×%d, want about %d×%d", i+1, cfg.Width, cfg.Height, want[0], want[1])
				}
			}

			// Cached: no renderer needed the second time.
			lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
			again, err := Render(context.Background(), pdf)
			if err != nil || len(again) != 2 || !bytes.Equal(again[1], pages[1]) {
				t.Errorf("cached render: %d pages, %v", len(again), err)
			}
			if entries, _ := os.ReadDir(cache); len(entries) != 1 {
				t.Errorf("cache holds %d entries, want the one deck", len(entries))
			}
		})
	}
}

// only makes tool the only renderer found.
func only(t *testing.T, tool, path string) {
	t.Cleanup(func() { lookPath = exec.LookPath })
	lookPath = func(name string) (string, error) {
		if name == tool {
			return path, nil
		}
		return "", exec.ErrNotFound
	}
}

func TestRenderWithoutRenderer(t *testing.T) {
	t.Setenv("ATLASSIAN_SLIDES_CACHE", t.TempDir())
	t.Cleanup(func() { lookPath = exec.LookPath })
	lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	if _, err := Render(context.Background(), twoPagePDF()); !errors.Is(err, ErrNoRenderer) {
		t.Errorf("got %v", err)
	}
}

func TestRenderBrokenPDF(t *testing.T) {
	path, err := exec.LookPath("mutool")
	tool := "mutool"
	if err != nil {
		if path, err = exec.LookPath("pdftoppm"); err != nil {
			t.Skip("no renderer installed")
		}
		tool = "pdftoppm"
	}
	only(t, tool, path)
	cache := t.TempDir()
	t.Setenv("ATLASSIAN_SLIDES_CACHE", cache)
	if _, err := Render(context.Background(), []byte("%PDF-1.4 garbage")); err == nil {
		t.Error("a broken PDF must fail")
	}
	if m, _ := filepath.Glob(filepath.Join(cache, "*")); len(m) != 0 {
		t.Errorf("a failed render must leave nothing in the cache: %v", m)
	}
}
