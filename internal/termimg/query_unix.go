//go:build unix

package termimg

import (
	"os"
	"regexp"
	"strings"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

var (
	xtversionRe = regexp.MustCompile(`\x1bP>\|([^\x1b]*)\x1b\\`)
	da1Re       = regexp.MustCompile(`\x1b\[\?([0-9;]*)c`)
)

// queryTerminal asks the terminal for its name and version (XTVERSION) and
// its device attributes (DA1, whose attribute 4 means Sixel). Every terminal
// answers DA1, which ends the wait; it gives up after a short timeout.
func queryTerminal() (version string, sixel bool) {
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return "", false
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", false
	}
	defer tty.Close()
	fd := int(tty.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return "", false
	}
	defer term.Restore(fd, state) //nolint:errcheck

	if _, err := tty.WriteString("\x1b[>0q\x1b[c"); err != nil {
		return "", false
	}
	var resp []byte
	buf := make([]byte, 256)
	deadline := time.Now().Add(400 * time.Millisecond)
	for time.Now().Before(deadline) {
		fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		n, err := unix.Poll(fds, int(time.Until(deadline).Milliseconds())+1)
		if err != nil || n == 0 {
			break
		}
		r, err := unix.Read(fd, buf)
		if err != nil || r <= 0 {
			break
		}
		resp = append(resp, buf[:r]...)
		if da1Re.Match(resp) {
			break
		}
	}
	if m := xtversionRe.FindSubmatch(resp); m != nil {
		version = string(m[1])
	}
	if m := da1Re.FindSubmatch(resp); m != nil {
		for _, a := range strings.Split(string(m[1]), ";") {
			if a == "4" {
				sixel = true
			}
		}
	}
	return version, sixel
}

// CellSize returns the size of a character cell in pixels, falling back to
// 8×16 when the terminal does not report its pixel size.
func CellSize() (w, h int) {
	ws, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ)
	if err == nil && ws.Col > 0 && ws.Row > 0 && ws.Xpixel > 0 && ws.Ypixel > 0 {
		return int(ws.Xpixel / ws.Col), int(ws.Ypixel / ws.Row)
	}
	return 8, 16
}
