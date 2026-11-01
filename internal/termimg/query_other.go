//go:build !unix

package termimg

func queryTerminal() (version string, sixel bool) { return "", false }

// CellSize returns an assumed character cell size in pixels.
func CellSize() (w, h int) { return 8, 16 }
