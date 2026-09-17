package main

// Thumbnails: decoded and downscaled once, cached as JPEG under <data>/thumbs.

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

type Thumbs struct {
	Dir   string
	sem   chan struct{}
	locks sync.Map // cache path -> *sync.Mutex, so one file is only rendered once
}

func NewThumbs(dir string) *Thumbs {
	return &Thumbs{Dir: dir, sem: make(chan struct{}, runtime.NumCPU())}
}

// Get returns the path of a cached JPEG no larger than size×size, turned upright per its EXIF orientation.
func (t *Thumbs) Get(src string, size, orientation int) (string, error) {
	st, err := os.Stat(src)
	if err != nil {
		return "", err
	}
	sum := sha1.Sum(fmt.Appendf(nil, "%s|%d|%d|%d|%d", src, st.ModTime().UnixNano(), st.Size(), size, orientation))
	key := hex.EncodeToString(sum[:])
	dst := filepath.Join(t.Dir, key[:2], key+".jpg")
	if _, err := os.Stat(dst); err == nil {
		return dst, nil
	}
	mu, _ := t.locks.LoadOrStore(dst, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	defer mu.(*sync.Mutex).Unlock()
	if _, err := os.Stat(dst); err == nil {
		return dst, nil
	}
	t.sem <- struct{}{}
	defer func() { <-t.sem }()

	img, err := decode(src)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	quality := 84
	if size >= 4000 {
		quality = 92 // zoomed-in viewing
	}
	if err := jpeg.Encode(&buf, orient(fit(img, size), orientation), &jpeg.Options{Quality: quality}); err != nil {
		return "", err
	}
	os.MkdirAll(filepath.Dir(dst), 0o755)
	if err := os.WriteFile(dst+".tmp", buf.Bytes(), 0o644); err != nil {
		return "", err
	}
	return dst, os.Rename(dst+".tmp", dst)
}

// decode tries Go's decoders, then the embedded preview exiftool can extract (DNG, HEIC, odd TIFFs).
func decode(src string) (image.Image, error) {
	if f, err := os.Open(src); err == nil {
		img, _, err := image.Decode(f)
		f.Close()
		if err == nil {
			return img, nil
		}
	}
	for _, tag := range []string{"-JpgFromRaw", "-PreviewImage", "-ThumbnailImage"} {
		if b, _ := exiftool("-b", tag, src); len(b) > 0 {
			if img, _, err := image.Decode(bytes.NewReader(b)); err == nil {
				return img, nil
			}
		}
	}
	return nil, errors.New("can't decode " + filepath.Base(src))
}

func fit(src image.Image, size int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= size && h <= size {
		return src
	}
	if w >= h {
		w, h = size, max(1, h*size/w)
	} else {
		w, h = max(1, w*size/h), size
	}
	// Big downscales: cheap pass to 2× target first, then a quality pass.
	if b.Dx() > 4*w {
		mid := image.NewRGBA(image.Rect(0, 0, w*2, h*2))
		draw.ApproxBiLinear.Scale(mid, mid.Bounds(), src, b, draw.Src, nil)
		src = mid
		b = mid.Bounds()
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	return dst
}
