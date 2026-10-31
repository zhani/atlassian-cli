package tui

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/viewport"
)

type keyMap struct {
	detail, open, copyURL, copyKey, transition, refresh, filter, help, back, quit, forceQuit key.Binding
	top, bottom, pageUp, pageDown, up, down                                                  key.Binding
	choose                                                                                   key.Binding
}

var keys = keyMap{
	detail:     key.NewBinding(key.WithKeys("enter", "v"), key.WithHelp("enter/v", "view")),
	open:       key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open in browser")),
	copyURL:    key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "copy URL")),
	copyKey:    key.NewBinding(key.WithKeys("ctrl+k"), key.WithHelp("ctrl+k", "copy key")),
	transition: key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "move (transition)")),
	refresh:    key.NewBinding(key.WithKeys("ctrl+r", "f5"), key.WithHelp("ctrl+r", "refresh")),
	filter:     key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
	help:       key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	back:       key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	quit:       key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
	forceQuit:  key.NewBinding(key.WithKeys("ctrl+c")),

	up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
	down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	top:      key.NewBinding(key.WithKeys("home", "g"), key.WithHelp("g", "top")),
	bottom:   key.NewBinding(key.WithKeys("end", "G"), key.WithHelp("G", "bottom")),
	pageUp:   key.NewBinding(key.WithKeys("ctrl+b", "pgup", "b"), key.WithHelp("ctrl+b", "page up")),
	pageDown: key.NewBinding(key.WithKeys("ctrl+f", "pgdown", "f", " "), key.WithHelp("ctrl+f", "page down")),
	choose:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "apply")),
}

// tableKeyMap adapts the shared movement bindings to the table component.
func tableKeyMap() table.KeyMap {
	km := table.DefaultKeyMap()
	km.LineUp = keys.up
	km.LineDown = keys.down
	km.PageUp = keys.pageUp
	km.PageDown = keys.pageDown
	km.GotoTop = keys.top
	km.GotoBottom = keys.bottom
	return km
}

// viewportKeyMap adapts the shared movement bindings to the detail viewport.
func viewportKeyMap() viewport.KeyMap {
	km := viewport.DefaultKeyMap()
	km.Up = keys.up
	km.Down = keys.down
	km.PageUp = keys.pageUp
	km.PageDown = keys.pageDown
	return km
}

// helpKeys implements help.KeyMap for the bindings of the current mode.
type helpKeys struct {
	short []key.Binding
	full  [][]key.Binding
}

func (h helpKeys) ShortHelp() []key.Binding  { return h.short }
func (h helpKeys) FullHelp() [][]key.Binding { return h.full }

func (m model) helpKeys() helpKeys {
	actions := []key.Binding{keys.open, keys.transition, keys.copyURL, keys.copyKey, keys.refresh}
	switch m.mode {
	case modeTransition:
		b := []key.Binding{keys.up, keys.down, keys.choose, keys.back}
		return helpKeys{short: b, full: [][]key.Binding{b}}
	case modeDetail:
		return helpKeys{
			short: []key.Binding{keys.back, keys.open, keys.transition, keys.help, keys.quit},
			full: [][]key.Binding{
				{keys.up, keys.down, keys.pageUp, keys.pageDown, keys.top, keys.bottom},
				actions,
				{keys.back, keys.help, keys.quit},
			},
		}
	default:
		return helpKeys{
			short: append(append([]key.Binding{keys.detail, keys.open, keys.transition, keys.filter}, m.extraHelp...), keys.help, keys.quit),
			full: [][]key.Binding{
				{keys.up, keys.down, keys.pageUp, keys.pageDown, keys.top, keys.bottom},
				append([]key.Binding{keys.detail}, actions...),
				{keys.filter, keys.back, keys.help, keys.quit},
			},
		}
	}
}
