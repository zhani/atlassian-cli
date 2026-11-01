package termimg

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

// Output is a terminal output shared by a TUI renderer and image escape
// sequences: writes are serialized so that an image transmission is never
// cut into a frame. It implements the file interface bubbletea needs to
// treat it as a terminal.
type Output struct {
	mu sync.Mutex
	f  *os.File
}

// NewOutput wraps a terminal file.
func NewOutput(f *os.File) *Output { return &Output{f: f} }

func (o *Output) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.f.Write(p)
}

// WriteSeqs writes escape sequences in one go.
func (o *Output) WriteSeqs(seqs ...string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, s := range seqs {
		if _, err := io.WriteString(o.f, s); err != nil {
			return err
		}
	}
	return nil
}

func (o *Output) Read(p []byte) (int, error) { return o.f.Read(p) }
func (o *Output) Close() error               { return nil }
func (o *Output) Fd() uintptr                { return o.f.Fd() }

// Kitty image IDs: the low byte is the 256-color foreground of the
// placeholders, the high byte (a diacritic) is random per process so that
// programs sharing a kitty window rarely collide.
var (
	kittyMu   sync.Mutex
	kittyNext uint32
	kittyHigh = uint32(time.Now().UnixNano()%255) + 1
)

// NewKittyID allocates an image ID for kitty placeholders.
func NewKittyID() uint32 {
	kittyMu.Lock()
	defer kittyMu.Unlock()
	kittyNext = kittyNext%255 + 1
	return kittyHigh<<24 | kittyNext
}

// PreviewItem is one image of a preview.
type PreviewItem struct {
	Title string
	Image *Image
}

// Preview shows images one at a time on the whole screen until Esc or q;
// n/p (or arrows, space) move between them. It implements bubbletea's
// ExecCommand, which hands it the terminal while the TUI is suspended.
type Preview struct {
	Caps  Caps
	Items []PreviewItem
	Index int

	in  io.Reader
	out io.Writer
}

func (p *Preview) SetStdin(r io.Reader)  { p.in = r }
func (p *Preview) SetStdout(w io.Writer) { p.out = w }
func (p *Preview) SetStderr(io.Writer)   {}

// Run draws the images and reads keys until the preview is closed.
func (p *Preview) Run() error {
	if len(p.Items) == 0 {
		return nil
	}
	in, out := p.in, p.out
	if in == nil {
		in = os.Stdin
	}
	if out == nil {
		out = os.Stdout
	}
	if f, ok := in.(interface{ Fd() uintptr }); ok && term.IsTerminal(int(f.Fd())) {
		state, err := term.MakeRaw(int(f.Fd()))
		if err == nil {
			defer term.Restore(int(f.Fd()), state) //nolint:errcheck
		}
	}
	io.WriteString(out, "\x1b[?1049h\x1b[?25l")              //nolint:errcheck
	defer io.WriteString(out, "\x1b[2J\x1b[?25h\x1b[?1049l") //nolint:errcheck

	var kittyIDs []uint32
	defer func() {
		for _, id := range kittyIDs {
			io.WriteString(out, p.Caps.Wrap(KittyDelete(id), false)) //nolint:errcheck
		}
	}()

	p.Index = min(max(0, p.Index), len(p.Items)-1)
	buf := make([]byte, 32)
	for {
		if id := p.draw(out); id != 0 {
			kittyIDs = append(kittyIDs, id)
		}
		n, err := in.Read(buf)
		if err != nil {
			return nil
		}
		switch k := string(buf[:n]); k {
		case "q", "\x1b", "i", "\r", "\x03":
			return nil
		case "n", "j", "l", " ", "\x1b[C", "\x1b[B", "\x1bOC", "\x1bOB":
			p.Index = (p.Index + 1) % len(p.Items)
		case "p", "k", "h", "\x7f", "\x1b[D", "\x1b[A", "\x1bOD", "\x1bOA":
			p.Index = (p.Index + len(p.Items) - 1) % len(p.Items)
		}
	}
}

// tmuxSettle is how long tmux gets to redraw a cleared pane before an image
// is sent through passthrough.
var tmuxSettle = 150 * time.Millisecond

// maxUpscale is how much the preview enlarges small images.
const maxUpscale = 3

// draw shows the current item; it returns the kitty image ID it created.
func (p *Preview) draw(out io.Writer) (kittyID uint32) {
	item := p.Items[p.Index]
	cols, rows := 80, 24
	if f, ok := out.(interface{ Fd() uintptr }); ok {
		if c, r, err := term.GetSize(int(f.Fd())); err == nil {
			cols, rows = c, r
		}
	}
	cellW, cellH := CellSize()
	imgW, imgH := item.Image.Size()
	avail := max(1, rows-1)

	// The cell box of the image, enlarging small images up to maxUpscale.
	var c, r int
	switch p.Caps.Preview {
	case Kitty:
		c, r = Fit(imgW*maxUpscale, imgH*maxUpscale, min(cols, MaxKittyCells), min(avail, MaxKittyCells), cellW, cellH)
	case ITerm, Sixel:
		c, r = Fit(imgW*maxUpscale, imgH*maxUpscale, cols, avail, cellW, cellH)
	default:
		c, r = Fit(imgW, imgH, cols, avail, 2, max(2, 2*cellH/max(1, cellW)))
	}
	left := (cols-c)/2 + 1

	// Encode first: Sixel may come out smaller than the box (it is shrunk
	// to fit tmux's limit), and is centered by its final size.
	var image string
	switch p.Caps.Preview {
	case ITerm, Sixel:
		// Pixel size of the box at the image's aspect ratio.
		h := max(1, min(r*cellH, c*cellW*imgH/imgW))
		w := max(1, h*imgW/imgH)
		limit := p.Caps.SeqLimit()
		if p.Caps.Preview == ITerm {
			// The terminal scales the image to the box: never send more
			// pixels than the image has.
			if w > imgW {
				w, h = imgW, imgH
			}
			image = p.Caps.Wrap(ITermSized(item.Image, w, h, c, r, limit), false)
		} else {
			seq, fw, _ := SixelSized(item.Image, w, h, limit)
			image = p.Caps.Wrap(seq, p.Caps.TmuxSixel)
			left = (cols-min(cols, (fw+cellW-1)/max(1, cellW)))/2 + 1
		}
	}

	// Clear the screen, show the caption and move the cursor to where the
	// image goes. tmux drops passthrough sequences that arrive while a
	// redraw of the pane is pending (as after a clear), and passthrough
	// images are drawn wherever tmux last put the terminal's cursor, so
	// inside tmux the image follows after a pause.
	caption := fmt.Sprintf(" [%d/%d] %s (%d×%d)   n/p: next/previous · esc: back", p.Index+1, len(p.Items), item.Title, imgW, imgH)
	if r := []rune(caption); len(r) > cols {
		caption = string(r[:cols])
	}
	io.WriteString(out, fmt.Sprintf("\x1b[H\x1b[2J\x1b[%d;1H\x1b[7m%s\x1b[0m\x1b[1;%dH", rows, caption, left)) //nolint:errcheck
	if p.Caps.Tmux {
		time.Sleep(tmuxSettle)
	}

	var sb strings.Builder
	switch p.Caps.Preview {
	case Kitty:
		kittyID = NewKittyID()
		for _, s := range KittyTransmit(kittyID, item.Image.PNG, c, r) {
			sb.WriteString(p.Caps.Wrap(s, false))
		}
		for i, line := range KittyPlaceholders(kittyID, c, r) {
			fmt.Fprintf(&sb, "\x1b[%d;%dH%s", i+1, left, line)
		}
	case ITerm, Sixel:
		sb.WriteString(image)
	default:
		for i, line := range HalfBlocks(item.Image, c, r) {
			fmt.Fprintf(&sb, "\x1b[%d;%dH%s", i+1, left, line)
		}
	}
	io.WriteString(out, sb.String()) //nolint:errcheck
	return kittyID
}
