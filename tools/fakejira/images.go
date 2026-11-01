package main

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"strconv"
)

// Demo images are drawn at startup (no binary files in the repository) and
// attached to the "Diagrams" page, so that page image display can be tried.

func fill(img draw.Image, r image.Rectangle, c color.Color) {
	draw.Draw(img, r, image.NewUniform(c), image.Point{}, draw.Src)
}

func frame(img draw.Image, r image.Rectangle, w int, c color.Color) {
	fill(img, image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+w), c)
	fill(img, image.Rect(r.Min.X, r.Max.Y-w, r.Max.X, r.Max.Y), c)
	fill(img, image.Rect(r.Min.X, r.Min.Y, r.Min.X+w, r.Max.Y), c)
	fill(img, image.Rect(r.Max.X-w, r.Min.Y, r.Max.X, r.Max.Y), c)
}

// architectureImage draws three connected boxes on a transparent background.
func architectureImage() []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 640, 300))
	ink := color.NRGBA{40, 50, 70, 255}
	boxes := []struct {
		r image.Rectangle
		c color.NRGBA
	}{
		{image.Rect(30, 100, 190, 200), color.NRGBA{120, 180, 240, 255}},
		{image.Rect(240, 40, 400, 140), color.NRGBA{140, 210, 140, 255}},
		{image.Rect(240, 170, 400, 270), color.NRGBA{250, 200, 110, 255}},
		{image.Rect(450, 100, 610, 200), color.NRGBA{230, 130, 140, 255}},
	}
	// connectors
	fill(img, image.Rect(190, 147, 215, 153), ink)
	fill(img, image.Rect(212, 87, 218, 223), ink)
	fill(img, image.Rect(215, 87, 240, 93), ink)
	fill(img, image.Rect(215, 217, 240, 223), ink)
	fill(img, image.Rect(400, 87, 425, 93), ink)
	fill(img, image.Rect(400, 217, 425, 223), ink)
	fill(img, image.Rect(422, 87, 428, 223), ink)
	fill(img, image.Rect(425, 147, 450, 153), ink)
	for _, b := range boxes {
		fill(img, b.r, b.c)
		frame(img, b.r, 4, ink)
		// "text" lines
		for y := b.r.Min.Y + 25; y < b.r.Max.Y-20; y += 18 {
			fill(img, image.Rect(b.r.Min.X+20, y, b.r.Max.X-20-(y%40), y+6), ink)
		}
	}
	return encodePNG(img)
}

// chartImage draws a bar chart.
func chartImage() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 480, 280))
	fill(img, img.Bounds(), color.White)
	axis := color.RGBA{60, 60, 60, 255}
	fill(img, image.Rect(40, 20, 44, 250), axis)
	fill(img, image.Rect(40, 246, 460, 250), axis)
	for y := 60; y < 246; y += 40 {
		fill(img, image.Rect(44, y, 460, y+1), color.RGBA{220, 220, 220, 255})
	}
	heights := []int{120, 180, 90, 210, 150, 60, 195}
	colors := []color.RGBA{{66, 133, 244, 255}, {219, 68, 55, 255}, {244, 180, 0, 255}, {15, 157, 88, 255}, {171, 71, 188, 255}, {0, 172, 193, 255}, {255, 112, 67, 255}}
	for i, h := range heights {
		x := 60 + i*56
		fill(img, image.Rect(x, 246-h, x+40, 246), colors[i])
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		log.Fatal(err)
	}
	return buf.Bytes()
}

// gradientImage draws a color wheel-like gradient (a "photo").
func gradientImage() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 400, 400))
	for y := 0; y < 400; y++ {
		for x := 0; x < 400; x++ {
			dx, dy := x-200, y-200
			if dx*dx+dy*dy > 190*190 {
				img.Set(x, y, color.White)
				continue
			}
			img.Set(x, y, color.RGBA{uint8(x * 255 / 400), uint8(y * 255 / 400), uint8(255 - x*255/400/2 - y*255/400/2), 255})
		}
	}
	return encodePNG(img)
}

// screenshotImage draws a large, noisy image like a real screenshot or
// photo: its PNG is well over tmux's 1 MiB escape sequence limit.
func screenshotImage() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	seed := uint32(7)
	for y := 0; y < 1080; y++ {
		for x := 0; x < 1920; x++ {
			seed = seed*1664525 + 1013904223
			n := int(seed>>27) - 16
			c := func(v int) uint8 { return uint8(min(255, max(0, v+n))) }
			img.Set(x, y, color.RGBA{c(x * 255 / 1920), c(80 + y*120/1080), c(200 - x*150/1920), 255})
		}
	}
	for i := 0; i < 6; i++ { // "windows"
		r := image.Rect(120+i*280, 150+(i%2)*380, 380+i*280, 450+(i%2)*380)
		fill(img, r, color.RGBA{245, 245, 245, 255})
		frame(img, r, 6, color.RGBA{30, 30, 50, 255})
	}
	return encodePNG(img)
}

func encodePNG(img image.Image) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		log.Fatal(err)
	}
	return buf.Bytes()
}

// attachDemoImages writes the demo images to a temporary directory and
// attaches them to a page; it returns the download path of each by name.
func (s *store) attachDemoImages(pageID string) map[string]string {
	dir, err := os.MkdirTemp("", "fakejira-images-")
	if err != nil {
		log.Fatal(err)
	}
	files := []struct {
		name, mime string
		data       []byte
	}{
		{"architecture.png", "image/png", architectureImage()},
		{"chart.jpg", "image/jpeg", chartImage()},
		{"gradient.png", "image/png", gradientImage()},
		{"screenshot.png", "image/png", screenshotImage()},
	}
	links := map[string]string{}
	for _, f := range files {
		path := filepath.Join(dir, f.name)
		if err := os.WriteFile(path, f.data, 0o600); err != nil {
			log.Fatal(err)
		}
		a := attachment{
			ID: strconv.Itoa(10000 + len(s.attachments)), Key: "page:" + pageID, Filename: f.name,
			Path: path, Size: int64(len(f.data)), MimeType: f.mime,
		}
		s.attachments[a.ID] = a
		links[f.name] = "/secure/attachment/" + a.ID + "/" + f.name
	}
	return links
}
