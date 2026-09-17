package main

import (
	"image"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotateOrientation(t *testing.T) {
	for _, c := range []struct{ from, deg, want int }{
		{1, 90, 6}, {6, 90, 3}, {3, 90, 8}, {8, 90, 1}, {1, -90, 8}, {0, 180, 3}, {2, 90, 7}, {6, 360, 6},
	} {
		if got := rotateOrientation(c.from, c.deg); got != c.want {
			t.Errorf("rotateOrientation(%d, %d) = %d, want %d", c.from, c.deg, got, c.want)
		}
	}
}

func TestOrientPixels(t *testing.T) {
	// 2×1 image: red on the left, blue on the right.
	src := image.NewRGBA(image.Rect(0, 0, 2, 1))
	red, blue := color.RGBA{255, 0, 0, 255}, color.RGBA{0, 0, 255, 255}
	src.Set(0, 0, red)
	src.Set(1, 0, blue)
	at := func(img image.Image, x, y int) color.RGBA { return color.RGBAModel.Convert(img.At(x, y)).(color.RGBA) }

	cw := orient(src, 6) // 90° clockwise: left edge goes to the top
	if b := cw.Bounds(); b.Dx() != 1 || b.Dy() != 2 || at(cw, 0, 0) != red || at(cw, 0, 1) != blue {
		t.Errorf("orientation 6 wrong: %v %v %v", cw.Bounds(), at(cw, 0, 0), at(cw, 0, 1))
	}
	ccw := orient(src, 8) // 90° counter-clockwise: left edge goes to the bottom
	if at(ccw, 0, 0) != blue || at(ccw, 0, 1) != red {
		t.Error("orientation 8 wrong")
	}
	if m := orient(src, 2); at(m, 0, 0) != blue {
		t.Error("orientation 2 should mirror")
	}
}

func TestRotateWritesOrientation(t *testing.T) {
	if _, err := exec.LookPath("exiftool"); err != nil {
		t.Skip("exiftool not installed")
	}
	root := t.TempDir()
	roll := filepath.Join(root, "lib", "Roll")
	os.MkdirAll(roll, 0o755)
	os.WriteFile(filepath.Join(roll, "a.jpg"), jpegBytes(t), 0o644)
	a := NewApp(filepath.Join(root, "data"), false)
	a.lib.Scan([]string{filepath.ToSlash(filepath.Join(root, "lib"))})
	file := filepath.ToSlash(filepath.Join(roll, "a.jpg"))

	p, err := a.lib.Rotate(file, 90)
	if err != nil || p.Orientation != 6 {
		t.Fatalf("after 90°: %+v %v", p, err)
	}
	if p, _ = a.lib.Rotate(file, -90); p.Orientation != 1 {
		t.Fatalf("rotating back: %+v", p)
	}
	thumb, err := a.thumbs.Get(filepath.FromSlash(file), 480, 6)
	if err != nil || !strings.HasSuffix(thumb, ".jpg") {
		t.Fatal(thumb, err)
	}
}

func TestFilmRankPrefersCurrentStocks(t *testing.T) {
	gold := toItem(1, strings.Split(";;Kodak Gold 200 Gen 6;;Kodak;;;;;;2;", ";"), nil)
	oldGold := toItem(2, strings.Split(";;Kodak GOLD 200 - Code : 6096 - GB;;Kodak;;;;;;0;", ";"), nil)
	obscure := toItem(3, strings.Split(";;Some Rebrand 200;;X;;;;;;2;", ";"), nil)
	if !(gold.rank() > obscure.rank() && obscure.rank() > oldGold.rank()) || gold.Popular == 0 || oldGold.Popular != 0 {
		t.Errorf("ranks: gold %d (popular %v), rebrand %d, discontinued gold %d", gold.rank(), gold.Popular, obscure.rank(), oldGold.rank())
	}
	if popularity("kodak ultra max 400 film gc400") == 0 || popularity("ultrafine ultramax t-grain 400") != 0 {
		t.Error("UltraMax should need the Kodak brand")
	}
	if popularity("kodak professional portra 400") <= popularity("adox scala 50") {
		t.Error("common stocks should outrank niche ones")
	}
}
