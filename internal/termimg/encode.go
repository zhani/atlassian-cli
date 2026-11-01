package termimg

import (
	"encoding/base64"
	"fmt"
	"image"
	"image/color/palette"
	"image/draw"
	"strconv"
	"strings"
)

// HalfBlocks renders the image as rows lines of cols half-block characters, each
// cell showing two pixels with 24-bit colors.
func HalfBlocks(img *Image, cols, rows int) []string {
	if cols <= 0 || rows <= 0 {
		return nil
	}
	px := Resize(img.RGBA, cols, rows*2)
	lines := make([]string, rows)
	var sb strings.Builder
	for row := 0; row < rows; row++ {
		sb.Reset()
		var lastFg, lastBg [3]uint8
		first := true
		for x := 0; x < cols; x++ {
			t := px.Pix[px.PixOffset(x, row*2):]
			b := px.Pix[px.PixOffset(x, row*2+1):]
			fg, bg := [3]uint8{t[0], t[1], t[2]}, [3]uint8{b[0], b[1], b[2]}
			if first || fg != lastFg {
				fmt.Fprintf(&sb, "\x1b[38;2;%d;%d;%dm", fg[0], fg[1], fg[2])
			}
			if first || bg != lastBg {
				fmt.Fprintf(&sb, "\x1b[48;2;%d;%d;%dm", bg[0], bg[1], bg[2])
			}
			lastFg, lastBg, first = fg, bg, false
			sb.WriteString("▀")
		}
		sb.WriteString("\x1b[0m")
		lines[row] = sb.String()
	}
	return lines
}

// placeholder is the kitty Unicode placeholder character.
const placeholder = '\U0010EEEE'

// MaxKittyCells is the most rows or columns a kitty placeholder grid can
// address.
const MaxKittyCells = len(diacritics) - 1

// KittyTransmit returns the escape sequences (one per chunk) that send a PNG
// to kitty as image id and create its virtual placement 1 of cols × rows
// cells for Unicode placeholders.
func KittyTransmit(id uint32, png []byte, cols, rows int) []string {
	data := base64.StdEncoding.EncodeToString(png)
	const chunk = 4096
	var out []string
	for i := 0; i < len(data); i += chunk {
		end := min(len(data), i+chunk)
		more := 0
		if end < len(data) {
			more = 1
		}
		if i == 0 {
			out = append(out, fmt.Sprintf("\x1b_Ga=T,U=1,f=100,t=d,i=%d,p=1,c=%d,r=%d,q=2,m=%d;%s\x1b\\", id, cols, rows, more, data[i:end]))
		} else {
			out = append(out, fmt.Sprintf("\x1b_Gm=%d,q=2;%s\x1b\\", more, data[i:end]))
		}
	}
	return out
}

// KittyPlace (re)creates virtual placement pid of image id with a new size.
func KittyPlace(id uint32, pid, cols, rows int) string {
	return fmt.Sprintf("\x1b_Ga=p,U=1,i=%d,p=%d,c=%d,r=%d,q=2\x1b\\", id, pid, cols, rows)
}

// KittyDelete frees image id in the terminal.
func KittyDelete(id uint32) string {
	return fmt.Sprintf("\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", id)
}

// KittyPlaceholders returns the text lines that show image id in place:
// placeholder characters whose 256-color foreground and diacritics encode
// the image id and the row and column of each cell. Text moves them like any
// other characters, so the image scrolls with the page, also inside tmux.
// Every cell carries its full position, as partial redraws (by tmux or the
// TUI) may start in the middle of a row.
func KittyPlaceholders(id uint32, cols, rows int) []string {
	cols, rows = min(cols, MaxKittyCells), min(rows, MaxKittyCells)
	low, high := id&0xff, (id>>24)&0xff
	idMark := ""
	if high != 0 {
		idMark = string(diacritics[high])
	}
	lines := make([]string, rows)
	var sb strings.Builder
	for r := 0; r < rows; r++ {
		sb.Reset()
		sb.WriteString("\x1b[38;5;" + strconv.Itoa(int(low)) + "m")
		for c := 0; c < cols; c++ {
			sb.WriteRune(placeholder)
			sb.WriteRune(diacritics[r])
			sb.WriteRune(diacritics[c])
			sb.WriteString(idMark)
		}
		sb.WriteString("\x1b[39m")
		lines[r] = sb.String()
	}
	return lines
}

// ITermImage returns the iTerm2 inline image sequence drawing the PNG in a
// cols × rows cell box at the cursor.
func ITermImage(png []byte, cols, rows int) string {
	return fmt.Sprintf("\x1b]1337;File=inline=1;size=%d;width=%d;height=%d;preserveAspectRatio=1:%s\a",
		len(png), cols, rows, base64.StdEncoding.EncodeToString(png))
}

// SixelImage encodes the image scaled to w×h pixels as a Sixel sequence.
func SixelImage(img *Image, w, h int) string {
	src := Resize(img.RGBA, w, h)
	pal := image.NewPaletted(src.Bounds(), palette.Plan9)
	draw.FloydSteinberg.Draw(pal, pal.Bounds(), src, image.Point{})

	var sb strings.Builder
	fmt.Fprintf(&sb, "\x1bP0;1;0q\"1;1;%d;%d", w, h)
	for i, c := range pal.Palette {
		r, g, b, _ := c.RGBA()
		fmt.Fprintf(&sb, "#%d;2;%d;%d;%d", i, r*100/0xffff, g*100/0xffff, b*100/0xffff)
	}
	bits := make([][]byte, len(pal.Palette))
	for y0 := 0; y0 < h; y0 += 6 {
		var used []int
		for y := y0; y < min(h, y0+6); y++ {
			for x := 0; x < w; x++ {
				c := pal.Pix[pal.PixOffset(x, y)]
				if bits[c] == nil {
					bits[c] = make([]byte, w)
					used = append(used, int(c))
				}
				bits[c][x] |= 1 << (y - y0)
			}
		}
		for i, c := range used {
			if i > 0 {
				sb.WriteByte('$')
			}
			fmt.Fprintf(&sb, "#%d", c)
			sixelRun(&sb, bits[c])
			bits[c] = nil
		}
		sb.WriteByte('-')
	}
	sb.WriteString("\x1b\\")
	return sb.String()
}

// sixelRun writes one color's sixels of a band, run-length encoded.
func sixelRun(sb *strings.Builder, row []byte) {
	for x := 0; x < len(row); {
		n := 1
		for x+n < len(row) && row[x+n] == row[x] {
			n++
		}
		ch := byte(63 + row[x])
		if n > 3 {
			fmt.Fprintf(sb, "!%d%c", n, ch)
		} else {
			for range n {
				sb.WriteByte(ch)
			}
		}
		x += n
	}
}
