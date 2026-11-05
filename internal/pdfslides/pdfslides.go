// Package pdfslides renders the pages of a PDF to PNG images, so that a PDF
// embedded in a page (the viewpdf macro, e.g. an offline copy of a Google
// Slides deck) can be shown as a slide show in a terminal. Go has no PDF
// renderer of its own, so this runs MuPDF's mutool or Poppler's pdftoppm,
// whichever is installed. Rendered slides are cached by the PDF's content
// hash, so a deck is rendered once per machine, not on every view.
package pdfslides

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Size is the longest side, in pixels, slides are rendered at.
const Size = 1600

// MaxPages caps how many pages of a PDF are rendered.
const MaxPages = 300

// ErrNoRenderer is returned when neither mutool nor pdftoppm is installed.
var ErrNoRenderer = errors.New("no PDF renderer: install mupdf-tools (mutool) or poppler-utils (pdftoppm)")

// IsPDF reports whether data looks like a PDF file.
func IsPDF(data []byte) bool {
	return bytes.HasPrefix(bytes.TrimLeft(data[:min(len(data), 1024)], " \t\r\n"), []byte("%PDF-"))
}

// lookPath is exec.LookPath; tests replace it.
var lookPath = exec.LookPath

// CacheDir is where rendered slides are kept: ATLASSIAN_SLIDES_CACHE, else
// atlassian-cli/slides in the user's cache directory. Empty when there is
// none, and then slides are rendered into a temporary directory each time.
func CacheDir() string {
	if d := os.Getenv("ATLASSIAN_SLIDES_CACHE"); d != "" {
		return d
	}
	d, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(d, "atlassian-cli", "slides")
}

// Render returns the PNG of each page of pdf (at most MaxPages), from the
// cache when the same PDF was rendered before.
func Render(ctx context.Context, pdf []byte) ([][]byte, error) {
	sum := sha256.Sum256(pdf)
	key := hex.EncodeToString(sum[:16])
	cache := CacheDir()
	if cache != "" {
		if pages, err := readPages(filepath.Join(cache, key)); err == nil && len(pages) > 0 {
			return pages, nil
		}
		if err := os.MkdirAll(cache, 0o755); err != nil {
			cache = ""
		}
	}
	tmp, err := os.MkdirTemp(cache, ".render-*") // cache "" = system temp dir
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	in := filepath.Join(tmp, "in.pdf")
	if err := os.WriteFile(in, pdf, 0o600); err != nil {
		return nil, err
	}
	out := filepath.Join(tmp, "out")
	if err := os.Mkdir(out, 0o755); err != nil {
		return nil, err
	}
	if err := render(ctx, in, out); err != nil {
		return nil, err
	}
	pages, err := readPages(out)
	if err != nil {
		return nil, err
	}
	if len(pages) == 0 {
		return nil, errors.New("the PDF has no pages")
	}
	if cache != "" {
		// Rename into place so a concurrent or interrupted render never
		// leaves a partial deck in the cache.
		_ = os.Rename(out, filepath.Join(cache, key))
	}
	return pages, nil
}

// render writes one PNG per page of in into dir, named so that sorting the
// names orders the pages.
func render(ctx context.Context, in, dir string) error {
	size, last := strconv.Itoa(Size), strconv.Itoa(MaxPages)
	var cmd *exec.Cmd
	if p, err := lookPath("mutool"); err == nil {
		// -w and -h together fit the page into the box, keeping its shape.
		cmd = exec.CommandContext(ctx, p, "draw", "-q", "-F", "png", "-w", size, "-h", size,
			"-o", filepath.Join(dir, "s-%04d.png"), in, "1-"+last)
	} else if p, err := lookPath("pdftoppm"); err == nil {
		cmd = exec.CommandContext(ctx, p, "-png", "-scale-to", size, "-l", last, in, filepath.Join(dir, "s"))
	} else {
		return ErrNoRenderer
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if i := strings.LastIndexByte(msg, '\n'); i >= 0 {
			msg = msg[i+1:]
		}
		if msg == "" {
			return fmt.Errorf("%s: %w", filepath.Base(cmd.Path), err)
		}
		return fmt.Errorf("%s: %s", filepath.Base(cmd.Path), msg)
	}
	return nil
}

// readPages reads the PNGs of dir in name order.
func readPages(dir string) ([][]byte, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".png") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	pages := make([][]byte, 0, len(names))
	for _, n := range names {
		b, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			return nil, err
		}
		pages = append(pages, b)
	}
	return pages, nil
}
