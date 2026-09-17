package main

// EXIF orientation: each of the 8 values is "mirror horizontally (or not), then rotate clockwise".

import "image"

type orientation struct {
	mirror bool
	deg    int // clockwise, applied after the mirror
}

var orientations = map[int]orientation{
	1: {false, 0}, 2: {true, 0}, 3: {false, 180}, 4: {true, 180},
	5: {true, 270}, 6: {false, 90}, 7: {true, 90}, 8: {false, 270},
}

// rotateOrientation returns the EXIF orientation after turning a photo clockwise by deg (any multiple of 90).
func rotateOrientation(o, deg int) int {
	cur, ok := orientations[o]
	if !ok {
		cur = orientations[1]
	}
	cur.deg = ((cur.deg+deg)%360 + 360) % 360
	for v, x := range orientations {
		if x == cur {
			return v
		}
	}
	return 1
}

// orient applies an EXIF orientation to decoded pixels, which Go's decoders ignore.
func orient(img image.Image, o int) image.Image {
	x, ok := orientations[o]
	if !ok || o == 1 {
		return img
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	ow, oh := w, h
	if x.deg == 90 || x.deg == 270 {
		ow, oh = h, w
	}
	out := image.NewRGBA(image.Rect(0, 0, ow, oh))
	for sy := 0; sy < h; sy++ {
		for sx := 0; sx < w; sx++ {
			px, py := sx, sy
			if x.mirror {
				px = w - 1 - sx
			}
			var dx, dy int
			switch x.deg {
			case 0:
				dx, dy = px, py
			case 90:
				dx, dy = h-1-py, px
			case 180:
				dx, dy = w-1-px, h-1-py
			case 270:
				dx, dy = py, w-1-px
			}
			out.Set(dx, dy, img.At(b.Min.X+sx, b.Min.Y+sy))
		}
	}
	return out
}
