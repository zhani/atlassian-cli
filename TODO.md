# TODO

## Image preview blank in WezTerm on Windows over SSH + tmux

`confluence browse` → `i` (full-screen image preview) shows nothing when the
terminal is **WezTerm on Windows**, connected over **SSH** to Ubuntu, running
inside **tmux**. In-page half-block images work there; native kitty works.

Known about the failing setup:

- WezTerm 20240203-110809-5046fc22 on Windows; tmux 3.6 on the remote host.
- `tmux display -p '#{client_termname} #{client_termtype}'` →
  `xterm-256color WezTerm 20240203-110809-5046fc22`
- `allow-passthrough` is `on`. A manual iTerm2 image works there:
  `printf '\033Ptmux;\033\033]1337;File=inline=1:%s\a\033\\\n' "$(base64 -w0 some.png)"`
- Earlier, tmux-native Sixel printed "SIXEL IMAGE (…)": tmux did not know
  that client supports Sixel (no `sixel` in `#{client_termfeatures}`).

Already fixed and verified with WezTerm **on Linux** inside tmux (screenshots),
but still blank on Windows:

- tmux drops passthrough sent while a pane redraw is pending → screen clear
  first, image after a 150 ms pause (`tmuxSettle` in `internal/termimg/preview.go`).
- Passthrough images are drawn at the terminal's last cursor position →
  cursor placed before the pause.
- Sequences over tmux `input-buffer-size` (1 MiB) are dropped → images are
  scaled/compressed below 90% of it (`SeqLimit`, `ITermSized`).

Next steps — reproduce the exact environment (Windows WezTerm → SSH →
tmux) and:

1. Check what was detected: add a debug line (e.g. `ATLASSIAN_IMAGES_DEBUG=1`
   printing `termimg.Caps` and the sizes sent) or log to a file.
2. Force each protocol: `ATLASSIAN_IMAGES=iterm|sixel|blocks` and compare.
3. Record what tmux sends to the client (`script` around `tmux attach`, as
   done on Linux) and diff against a working manual `printf`.
4. Suspects: Windows ConPTY/SSH mangling long or DCS-wrapped sequences,
   timing (150 ms too short over the network), the cell pixel size reported
   over SSH (`TIOCGWINSZ` pixels 0 → 8×16 default), WezTerm on Windows
   handling `width=`/`height=` in cells, or the alternate screen.
