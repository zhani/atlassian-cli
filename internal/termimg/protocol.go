// Package termimg draws images in terminals: as colored half-block text that
// works anywhere, or through the kitty, iTerm2 and Sixel image protocols,
// wrapped for tmux passthrough when needed.
package termimg

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Protocol is a way of drawing images in a terminal.
type Protocol int

const (
	Off    Protocol = iota // no images, only captions
	Blocks                 // colored half-block characters (any truecolor terminal)
	Kitty                  // kitty graphics protocol with Unicode placeholders
	ITerm                  // iTerm2 inline images (WezTerm)
	Sixel                  // DEC Sixel (foot, WezTerm, Windows Terminal, tmux 3.4+)
)

func (p Protocol) String() string {
	return [...]string{"off", "blocks", "kitty", "iterm", "sixel"}[p]
}

// ParseProtocol parses an ATLASSIAN_IMAGES value; "" and "auto" return ok
// false so that the protocol is detected.
func ParseProtocol(s string) (p Protocol, ok bool, err error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "auto":
		return Off, false, nil
	case "off", "none", "0", "false":
		return Off, true, nil
	case "blocks", "halfblocks", "text":
		return Blocks, true, nil
	case "kitty":
		return Kitty, true, nil
	case "iterm", "iterm2", "wezterm":
		return ITerm, true, nil
	case "sixel":
		return Sixel, true, nil
	}
	return Off, false, fmt.Errorf("ATLASSIAN_IMAGES: unknown value %q (use auto, kitty, iterm, sixel, blocks or off)", s)
}

// Caps describes how images can be shown in the current terminal.
type Caps struct {
	// InPage draws images inside scrolling text: Kitty (placeholders move
	// with the text, also inside tmux), Blocks or Off.
	InPage Protocol
	// Preview draws one image on an otherwise empty screen.
	Preview Protocol

	Tmux        bool // running inside tmux
	Passthrough bool // tmux forwards escape sequences (allow-passthrough on)
	// TmuxSixel: tmux draws Sixel itself for this client (no passthrough
	// needed). tmux needs Sixel support and must know that the client
	// terminal has the sixel feature; otherwise it shows "SIXEL IMAGE".
	TmuxSixel bool
	Terminal  string
	// TmuxBuffer is tmux's input-buffer-size: longer escape sequences are
	// dropped.
	TmuxBuffer int

	blocked bool // a better protocol was given up for lack of passthrough
}

// Wrap prepares an escape sequence for the terminal: inside tmux it is sent
// through passthrough, unless tmux handles it itself (Sixel).
func (c Caps) Wrap(seq string, native bool) string {
	if !c.Tmux || native {
		return seq
	}
	return TmuxWrap(seq)
}

// TmuxWrap wraps an escape sequence in a tmux passthrough DCS sequence.
func TmuxWrap(seq string) string {
	return "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
}

// Detect works out the image protocols to use. ATLASSIAN_IMAGES forces one
// (kitty, iterm, sixel, blocks, off); otherwise the terminal is identified
// from the environment, by asking tmux about its client, or by querying the
// terminal (XTVERSION and DA1) when stdin and stdout are a terminal.
func Detect() (Caps, error) {
	var c Caps
	if os.Getenv("TMUX") != "" {
		c.Tmux = true
		c.Terminal, c.Passthrough, c.TmuxSixel, c.TmuxBuffer = tmuxInfo()
	}

	forced, ok, err := ParseProtocol(os.Getenv("ATLASSIAN_IMAGES"))
	if err != nil {
		return c, err
	}
	if ok {
		c.Preview = forced
		c.InPage = Blocks
		switch forced {
		case Off:
			c.InPage = Off
		case Kitty:
			c.InPage = Kitty
		}
		return c.passthroughFallback(), nil
	}

	term := strings.ToLower(os.Getenv("TERM"))
	sixel := false
	switch {
	case c.Tmux:
		// TERM describes tmux; the client terminal comes from tmux.
		term = strings.ToLower(c.Terminal)
	case os.Getenv("KITTY_WINDOW_ID") != "" || term == "xterm-kitty":
		term = "kitty"
	case os.Getenv("TERM_PROGRAM") == "WezTerm" || term == "wezterm":
		term = "wezterm"
	case strings.HasPrefix(term, "foot"):
		term = "foot"
	default:
		var version string
		version, sixel = queryTerminal()
		term = strings.ToLower(version)
		c.Terminal = version
	}

	return c.choose(term, sixel), nil
}

// choose picks the protocols for a terminal (lower-case name or version)
// and whether it reported Sixel support.
func (c Caps) choose(term string, sixel bool) Caps {
	switch {
	case strings.Contains(term, "kitty") || strings.Contains(term, "ghostty"):
		c.InPage, c.Preview = Kitty, Kitty
	case c.Tmux && c.TmuxSixel:
		c.InPage, c.Preview = Blocks, Sixel
	case strings.Contains(term, "wezterm"):
		c.InPage, c.Preview = Blocks, ITerm
	case sixel || strings.Contains(term, "foot"):
		c.InPage, c.Preview = Blocks, Sixel
	default:
		c.InPage, c.Preview = Blocks, Blocks
	}
	return c.passthroughFallback()
}

// defaultTmuxBuffer is tmux's default input-buffer-size (1 MiB).
const defaultTmuxBuffer = 1 << 20

// SeqLimit is the longest escape sequence that reaches the terminal, or 0
// for no limit. Inside tmux it leaves room for the passthrough wrapping.
func (c Caps) SeqLimit() int {
	if !c.Tmux {
		return 0
	}
	n := c.TmuxBuffer
	if n <= 0 {
		n = defaultTmuxBuffer
	}
	return n * 9 / 10
}

// passthroughFallback replaces protocols that cannot get through tmux.
func (c Caps) passthroughFallback() Caps {
	if !c.Tmux || c.Passthrough {
		return c
	}
	if c.InPage == Kitty {
		c.InPage, c.blocked = Blocks, true
	}
	switch c.Preview {
	case Kitty, ITerm:
		c.Preview, c.blocked = Blocks, true
	case Sixel:
		if !c.TmuxSixel {
			c.Preview, c.blocked = Blocks, true
		}
	}
	return c
}

// Hint explains how to get better images, or "".
func (c Caps) Hint() string {
	if c.blocked {
		return "set -g allow-passthrough on in tmux for full-resolution images"
	}
	return ""
}

// tmuxInfo asks tmux for the client terminal ("termname termtype features"),
// whether passthrough is allowed and whether tmux has Sixel support.
func tmuxInfo() (terminal string, passthrough, sixel bool, buffer int) {
	out, err := exec.Command("tmux", "display", "-p", "#{client_termname} #{client_termtype}|#{client_termfeatures}|#{sixel_support}").Output()
	if err == nil {
		parts := strings.SplitN(strings.TrimSpace(string(out)), "|", 3)
		if len(parts) == 3 {
			terminal = strings.TrimSpace(parts[0])
			clientSixel := false
			for _, f := range strings.Split(parts[1], ",") {
				clientSixel = clientSixel || f == "sixel"
			}
			sixel = clientSixel && parts[2] == "1"
		}
	}
	if out, err := exec.Command("tmux", "show", "-Apv", "allow-passthrough").Output(); err == nil {
		v := strings.TrimSpace(string(out))
		passthrough = v == "on" || v == "all"
	}
	buffer = defaultTmuxBuffer
	if out, err := exec.Command("tmux", "show", "-sv", "input-buffer-size").Output(); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(string(out))); err == nil && n > 0 {
			buffer = n
		}
	}
	return terminal, passthrough, sixel, buffer
}
