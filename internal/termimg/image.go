package termimg

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif" // register decoders
	_ "image/jpeg"
	"image/png"
)

// MaxPixels bounds the size of images that are decoded.
const MaxPixels = 40_000_000

// maxSide is the longest side images are kept at after decoding; terminals
// never need more.
const maxSide = 2000

// Image is a decoded image flattened onto white (as Confluence shows
// transparent images) together with a PNG encoding for terminal protocols.
type Image struct {
	RGBA *image.RGBA
	PNG  []byte
}

// Decode decodes PNG, JPEG or GIF data.
func Decode(data []byte) (*Image, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("unsupported image: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > MaxPixels {
		return nil, fmt.Errorf("%s image too large (%d×%d)", format, cfg.Width, cfg.Height)
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	b := src.Bounds()
	flat := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(flat, flat.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(flat, flat.Bounds(), src, b.Min, draw.Over)
	if w, h := b.Dx(), b.Dy(); w > maxSide || h > maxSide {
		if w >= h {
			flat = Resize(flat, maxSide, max(1, h*maxSide/w))
		} else {
			flat = Resize(flat, max(1, w*maxSide/h), maxSide)
		}
	}
	var buf bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&buf, flat); err != nil {
		return nil, err
	}
	return &Image{RGBA: flat, PNG: buf.Bytes()}, nil
}

// Size returns the image size in pixels.
func (img *Image) Size() (w, h int) {
	b := img.RGBA.Bounds()
	return b.Dx(), b.Dy()
}

// Fit returns the cell box (cols × rows) an image of imgW×imgH pixels takes
// when scaled to fit maxCols × maxRows cells of cellW×cellH pixels, keeping
// its aspect ratio and never enlarging it beyond its own size.
func Fit(imgW, imgH, maxCols, maxRows, cellW, cellH int) (cols, rows int) {
	if imgW <= 0 || imgH <= 0 || maxCols <= 0 || maxRows <= 0 {
		return 0, 0
	}
	cellW, cellH = max(1, cellW), max(1, cellH)
	cols = min(maxCols, (imgW+cellW-1)/cellW)
	rows = max(1, (cols*cellW*imgH/imgW+cellH-1)/cellH)
	if rows > maxRows {
		rows = maxRows
		cols = max(1, rows*cellH*imgW/imgH/cellW)
		cols = min(cols, maxCols)
	}
	return cols, rows
}

// Resize scales src to w×h, averaging the covered source pixels when
// shrinking (and repeating them when enlarging).
func Resize(src *image.RGBA, w, h int) *image.RGBA {
	w, h = max(1, w), max(1, h)
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	for y := 0; y < h; y++ {
		y0 := y * sh / h
		y1 := max(y0+1, (y+1)*sh/h)
		for x := 0; x < w; x++ {
			x0 := x * sw / w
			x1 := max(x0+1, (x+1)*sw/w)
			var r, g, b, a, n uint32
			for sy := y0; sy < y1; sy++ {
				off := src.PixOffset(sb.Min.X+x0, sb.Min.Y+sy)
				for sx := x0; sx < x1; sx++ {
					p := src.Pix[off : off+4 : off+4]
					r += uint32(p[0])
					g += uint32(p[1])
					b += uint32(p[2])
					a += uint32(p[3])
					n++
					off += 4
				}
			}
			d := dst.PixOffset(x, y)
			dst.Pix[d] = uint8(r / n)
			dst.Pix[d+1] = uint8(g / n)
			dst.Pix[d+2] = uint8(b / n)
			dst.Pix[d+3] = uint8(a / n)
		}
	}
	return dst
}
