package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
)

// generateTrayIcon creates a 16x16 envelope icon for the system tray.
func generateTrayIcon() []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 16, 16))

	bg := color.NRGBA{R: 66, G: 133, B: 244, A: 255}  // Google blue
	fg := color.NRGBA{R: 255, G: 255, B: 255, A: 255} // white

	// Fill background
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.SetNRGBA(x, y, bg)
		}
	}

	// White envelope body (filled rectangle)
	for y := 3; y <= 12; y++ {
		for x := 2; x <= 13; x++ {
			img.SetNRGBA(x, y, fg)
		}
	}

	// Blue diagonal lines on top of body — creates envelope flap (V shape)
	// Left:  (2,3) → (7,8)
	// Right: (13,3) → (8,8)
	for i := 0; i <= 5; i++ {
		img.SetNRGBA(2+i, 3+i, bg)
		img.SetNRGBA(13-i, 3+i, bg)
	}

	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}
