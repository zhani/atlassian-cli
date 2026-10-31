package tui

import (
	"os"

	"github.com/atotto/clipboard"
	osc52 "github.com/aymanbagabas/go-osc52/v2"
)

// CopyToClipboard copies text using the system clipboard tool (pbcopy,
// xclip, xsel, wl-copy, ...). When none is available it falls back to the
// OSC 52 escape sequence, which most modern terminals (including over SSH)
// turn into a clipboard write.
func CopyToClipboard(text string) error {
	if err := clipboard.WriteAll(text); err == nil {
		return nil
	}
	_, err := osc52.New(text).WriteTo(os.Stderr)
	return err
}
