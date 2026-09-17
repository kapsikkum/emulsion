package main

// Photo library: one folder = one roll. Metadata lives in the photos (EXIF/XMP via exiftool),
// or in XMP sidecars for camera RAW files, the same way Lightroom does it.
// library.json is only a cache of the last scan so startup is instant.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	// Formats exiftool can write in place.
	imageExts = []string{"jpg", "jpeg", "tif", "tiff", "png", "dng", "webp", "heic", "jxl"}
	// Camera RAW (camera scanning): metadata goes into a .xmp sidecar, never the RAW itself.
	rawExts = []string{"nef", "nrw", "cr2", "cr3", "crw", "arw", "srf", "sr2", "raf", "orf", "rw2", "pef", "srw", "3fr", "fff", "iiq", "rwl", "x3f", "erf", "mef", "mos", "kdc", "dcr"}
)

func ext(name string) string { return strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".") }
func isRaw(name string) bool { return slices.Contains(rawExts, ext(name)) }
func isImage(name string) bool {
	return slices.Contains(imageExts, ext(name)) || isRaw(name)
}

// sidecarFor returns the XMP sidecar of a RAW file: an existing IMG.NEF.xmp (darktable) or IMG.xmp (Lightroom).
func sidecarFor(raw string) string {
	if _, err := os.Stat(raw + ".xmp"); err == nil {
		return raw + ".xmp"
	}
	return strings.TrimSuffix(raw, filepath.Ext(raw)) + ".xmp"
}

// text accepts any JSON scalar; exiftool emits numeric-looking values as numbers.
type text string

func (t *text) UnmarshalJSON(b []byte) error {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	if v != nil {
		*t = text(strings.TrimSpace(fmt.Sprint(v)))
	}
	return nil
}

type Photo struct {
	SourceFile                                    string
	Make, Model, LensModel, ISO, DateTimeOriginal text
	FilmStock                                     text  `json:",omitempty"` // from NegPy exports
	ImageWidth, ImageHeight                       int   `json:",omitempty"`
	FileSize                                      int64 `json:",omitempty"`
	PreservedFileName                             text  `json:",omitempty"` // original name before an import rename
	Subject                                       any   `json:",omitempty"` // string or list
	Export                                        bool  `json:",omitempty"` // lives in a converted/export sub-folder
}

// Film stock is stored as an XMP keyword "film:<name>" so Lightroom & co. see it too.
func (p Photo) Film() string {
	tags, ok := p.Subject.([]any)
	if !ok {
		tags = []any{p.Subject}
	}
	for _, t := range tags {
		if s := fmt.Sprint(t); strings.HasPrefix(s, "film:") {
			return strings.TrimSpace(s[5:])
		}
	}
	return string(p.FilmStock)
}

func (p Photo) Camera() string { return strings.TrimSpace(string(p.Make + " " + p.Model)) }

// rawMeta is exiftool's JSON, including NegPy's capture tags (namespace https://negpy.app/ns/1.0/).
type rawMeta struct {
	Photo
	CaptureFilmStock, CaptureCameraMake, CaptureCameraModel, CaptureLensModel, CaptureFilmISO text
}

func exiftool(args ...string) ([]byte, error) {
	cmd := exec.Command("exiftool", "-@", "-") // args via stdin: no command-line length limit
	hideWindow(cmd)
	cmd.Stdin = strings.NewReader(strings.Join(append([]string{"-charset", "filename=utf8"}, args...), "\n"))
	out, err := cmd.Output()
	if ee, ok := err.(*exec.ExitError); ok {
		err = fmt.Errorf("%w: %s", err, bytes.TrimSpace(ee.Stderr))
	}
	return out, err
}

type Job struct {
	Kind, Message string
	Done, Total   int
	Running       bool
	Error         string
	Dirs          []string `json:",omitempty"` // roll folders created by the last import
}

type Library struct {
	cacheFile  string
	mu         sync.RWMutex
	photos     map[string]Photo // keyed by SourceFile (forward slashes)
	status     string
	job        Job
	jobMu      sync.Mutex
	exportDirs func() []string
}

func NewLibrary(dataDir string, exportDirs func() []string) *Library {
	l := &Library{cacheFile: filepath.Join(dataDir, "library.json"), photos: map[string]Photo{}, exportDirs: exportDirs}
	if b, err := os.ReadFile(l.cacheFile); err == nil {
		var ps []Photo
		if json.Unmarshal(b, &ps) == nil {
			for _, p := range ps {
				l.photos[p.SourceFile] = p
			}
		}
	}
	return l
}

func (l *Library) Status() string { l.mu.RLock(); defer l.mu.RUnlock(); return l.status }

func (l *Library) setStatus(s string) {
	l.mu.Lock()
	l.status = s
	l.mu.Unlock()
	logf("library: %s", s)
}

func (l *Library) Job() Job { l.jobMu.Lock(); defer l.jobMu.Unlock(); return l.job }

func (l *Library) setJob(f func(*Job)) { l.jobMu.Lock(); f(&l.job); l.jobMu.Unlock() }

func (l *Library) Photo(file string) (Photo, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	p, ok := l.photos[file]
	return p, ok
}

func (l *Library) isExportDir(dir string) bool {
	return slices.ContainsFunc(l.exportDirs(), func(e string) bool { return strings.EqualFold(e, path.Base(dir)) })
}

// rollDir maps a photo to its roll: export sub-folders belong to the parent roll.
func (l *Library) rollDir(file string) (dir string, export bool) {
	d := path.Dir(file)
	if l.isExportDir(d) {
		return path.Dir(d), true
	}
	return d, false
}

var scanTags = []string{"-j", "-q", "-q", "-d", "%Y-%m-%d", "-Make", "-Model", "-LensModel", "-ISO", "-DateTimeOriginal", "-ImageWidth", "-ImageHeight", "-FileSize#", "-PreservedFileName", "-XMP-dc:Subject",
	"-CaptureFilmStock", "-CaptureCameraMake", "-CaptureCameraModel", "-CaptureLensModel", "-CaptureFilmISO"}

func readMeta(recursive bool, paths ...string) ([]Photo, error) {
	args := slices.Clone(scanTags)
	for _, e := range slices.Concat(imageExts, rawExts, []string{"xmp"}) {
		args = append(args, "-ext", e)
	}
	if recursive {
		args = append(args, "-r")
	}
	out, err := exiftool(append(args, paths...)...)
	var ee *exec.ExitError
	if err != nil && !errors.As(err, &ee) {
		return nil, errors.New("exiftool is not installed or not on PATH")
	}
	var raws []rawMeta
	if len(bytes.TrimSpace(out)) > 0 {
		if err := json.Unmarshal(out, &raws); err != nil {
			return nil, err
		}
	}
	return mergeMeta(raws), nil
}

// mergeMeta folds XMP sidecars into their RAW files and NegPy capture tags into the standard fields.
func mergeMeta(raws []rawMeta) []Photo {
	sidecars := map[string]rawMeta{}
	for _, r := range raws {
		if f := filepath.ToSlash(r.SourceFile); ext(f) == "xmp" {
			sidecars[strings.ToLower(f)] = r
		}
	}
	var ps []Photo
	for _, r := range raws {
		f := filepath.ToSlash(r.SourceFile)
		if ext(f) == "xmp" {
			continue
		}
		p := r.Photo
		p.SourceFile = f
		if isRaw(f) {
			for _, sc := range []string{f + ".xmp", strings.TrimSuffix(f, path.Ext(f)) + ".xmp"} {
				if s, ok := sidecars[strings.ToLower(sc)]; ok {
					override(&p, s)
					break
				}
			}
		}
		// NegPy writes the *film* camera to Capture* and leaves Make/Model as the scanning camera.
		for _, kv := range []struct {
			dst *text
			src text
		}{{&p.Make, r.CaptureCameraMake}, {&p.Model, r.CaptureCameraModel}, {&p.LensModel, r.CaptureLensModel}, {&p.ISO, r.CaptureFilmISO}, {&p.FilmStock, r.CaptureFilmStock}} {
			if kv.src != "" {
				*kv.dst = kv.src
			}
		}
		ps = append(ps, p)
	}
	return ps
}

// override copies non-empty sidecar values over the RAW's embedded ones (the sidecar is what Lightroom trusts).
func override(p *Photo, s rawMeta) {
	for _, kv := range []struct {
		dst *text
		src text
	}{{&p.Make, s.Make}, {&p.Model, s.Model}, {&p.LensModel, s.LensModel}, {&p.ISO, s.ISO}, {&p.DateTimeOriginal, s.DateTimeOriginal}, {&p.PreservedFileName, s.PreservedFileName}} {
		if kv.src != "" {
			*kv.dst = kv.src
		}
	}
	if s.Subject != nil {
		p.Subject = s.Subject
	}
}

// Scan re-reads every library root.
func (l *Library) Scan(roots []string) {
	l.setStatus("Scanning photos…")
	var ps []Photo
	var err error
	if len(roots) > 0 {
		ps, err = readMeta(true, roots...)
	}
	if err != nil {
		l.setStatus("Scan failed: " + err.Error())
		return
	}
	m := map[string]Photo{}
	for _, p := range ps {
		m[p.SourceFile] = p
	}
	l.mu.Lock()
	l.photos = m
	l.mu.Unlock()
	l.save()
	l.setStatus(fmt.Sprintf("%d photos indexed", len(ps)))
}

// rescanDir refreshes one roll folder and its export sub-folders.
func (l *Library) rescanDir(dir string) error {
	paths := []string{filepath.FromSlash(dir)}
	if entries, err := os.ReadDir(filepath.FromSlash(dir)); err == nil {
		for _, e := range entries {
			if e.IsDir() && l.isExportDir(e.Name()) {
				paths = append(paths, filepath.Join(filepath.FromSlash(dir), e.Name()))
			}
		}
	}
	ps, err := readMeta(false, paths...)
	if err != nil {
		return err
	}
	l.mu.Lock()
	for k := range l.photos {
		if d, _ := l.rollDir(k); d == dir || path.Dir(k) == dir {
			delete(l.photos, k)
		}
	}
	for _, p := range ps {
		l.photos[p.SourceFile] = p
	}
	n := len(l.photos)
	l.mu.Unlock()
	l.save()
	l.setStatus(fmt.Sprintf("%d photos indexed", n))
	return nil
}

func (l *Library) save() {
	l.mu.RLock()
	ps := make([]Photo, 0, len(l.photos))
	for _, p := range l.photos {
		ps = append(ps, p)
	}
	l.mu.RUnlock()
	if b, err := json.Marshal(ps); err == nil {
		os.WriteFile(l.cacheFile+".tmp", b, 0o644)
		os.Rename(l.cacheFile+".tmp", l.cacheFile)
	}
}

type Roll struct {
	Dir, Name, Date              string
	Frames                       []Photo `json:",omitempty"`
	Exports                      []Photo `json:",omitempty"`
	Count, ExportCount, RawCount int
	Cover                        string
	Films, Cameras, Lenses, ISO  []string
}

func addUniq(list []string, s string) []string {
	if s = strings.TrimSpace(s); s == "" || slices.Contains(list, s) {
		return list
	}
	return append(list, s)
}

// Rolls groups photos by folder, newest first.
func (l *Library) Rolls(withFrames bool) []*Roll {
	l.mu.RLock()
	defer l.mu.RUnlock()
	m := map[string]*Roll{}
	rs := []*Roll{}
	for _, p := range l.photos {
		d, export := l.rollDir(p.SourceFile)
		r := m[d]
		if r == nil {
			r = &Roll{Dir: d, Name: path.Base(d), Films: []string{}, Cameras: []string{}, Lenses: []string{}, ISO: []string{}}
			m[d] = r
			rs = append(rs, r)
		}
		p.Export = export
		if export {
			r.Exports = append(r.Exports, p)
		} else {
			r.Frames = append(r.Frames, p)
			if isRaw(p.SourceFile) {
				r.RawCount++
			}
		}
		r.Films = addUniq(r.Films, p.Film())
		r.Cameras = addUniq(r.Cameras, p.Camera())
		r.Lenses = addUniq(r.Lenses, string(p.LensModel))
		r.ISO = addUniq(r.ISO, string(p.ISO))
		if dt := string(p.DateTimeOriginal); len(dt) >= 10 && !strings.HasPrefix(dt, "0000") && (r.Date == "" || dt < r.Date) {
			r.Date = dt[:10]
		}
	}
	byName := func(ps []Photo) {
		sort.Slice(ps, func(i, j int) bool { return naturalLess(ps[i].SourceFile, ps[j].SourceFile) })
	}
	for _, r := range rs {
		byName(r.Frames)
		byName(r.Exports)
		r.Count, r.ExportCount = len(r.Frames), len(r.Exports)
		// Converted positives make a better cover than an orange negative.
		if len(r.Exports) > 0 {
			r.Cover = r.Exports[0].SourceFile
		} else {
			r.Cover = r.Frames[0].SourceFile
		}
		if !withFrames {
			r.Frames, r.Exports = nil, nil
		}
	}
	sort.Slice(rs, func(i, j int) bool {
		if rs[i].Date != rs[j].Date {
			return rs[i].Date > rs[j].Date
		}
		return naturalLess(rs[i].Name, rs[j].Name)
	})
	return rs
}

func (l *Library) Roll(dir string) *Roll {
	for _, r := range l.Rolls(true) {
		if r.Dir == dir {
			return r
		}
	}
	return nil
}

// naturalLess sorts "frame2" before "frame10".
func naturalLess(a, b string) bool {
	a, b = strings.ToLower(a), strings.ToLower(b)
	for a != "" && b != "" {
		da, db := digits(a), digits(b)
		if da != "" && db != "" {
			na, nb := strings.TrimLeft(da, "0"), strings.TrimLeft(db, "0")
			if len(na) != len(nb) {
				return len(na) < len(nb)
			}
			if na != nb {
				return na < nb
			}
			a, b = a[len(da):], b[len(db):]
			continue
		}
		if a[0] != b[0] {
			return a[0] < b[0]
		}
		a, b = a[1:], b[1:]
	}
	return len(a) < len(b)
}

func digits(s string) string {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return s[:i]
}

type Meta struct {
	Film, ISO, Make, Model, Lens, Date string
}

// metaArgs builds exiftool write args; blank fields are left unchanged.
// Generic tag names let exiftool pick EXIF for images and XMP for sidecars.
func metaArgs(m Meta, oldFilms []string) ([]string, error) {
	args := []string{"-overwrite_original", "-q"}
	for _, kv := range [][2]string{{"Make", m.Make}, {"Model", m.Model}, {"LensModel", m.Lens}, {"ISO", m.ISO}} {
		if v := strings.TrimSpace(kv[1]); v != "" {
			args = append(args, "-"+kv[0]+"="+v)
		}
	}
	if d := strings.TrimSpace(m.Date); d != "" {
		t, err := time.Parse("2006-01-02", d)
		if err != nil {
			return nil, errors.New("date must be YYYY-MM-DD")
		}
		args = append(args, "-DateTimeOriginal="+t.Format("2006:01:02")+" 12:00:00")
	}
	if film := strings.TrimSpace(m.Film); film != "" {
		for _, f := range oldFilms {
			args = append(args, "-XMP-dc:Subject-=film:"+f)
		}
		args = append(args, "-XMP-dc:Subject+=film:"+film)
	}
	for _, a := range args {
		if strings.ContainsAny(a, "\r\n") {
			return nil, errors.New("values can't contain line breaks")
		}
	}
	return args, nil
}

// writeMeta tags files in place, or their XMP sidecars for RAW files (created when missing).
func writeMeta(files []string, m Meta, oldFilms []string) error {
	args, err := metaArgs(m, oldFilms)
	if err != nil || len(args) == 2 {
		return err
	}
	targets, err := metaTargets(files)
	if err != nil || len(targets) == 0 {
		return err
	}
	_, err = exiftool(append(args, targets...)...)
	return err
}

// preserveNames records renamed files' original names, as Lightroom does, so duplicate checks still match.
func preserveNames(originals map[string]string) error {
	var files []string
	for f, orig := range originals {
		if !strings.ContainsAny(orig, "\r\n") {
			files = append(files, f)
		}
	}
	targets, err := metaTargets(files)
	if err != nil || len(targets) == 0 {
		return err
	}
	var args []string
	for i, t := range targets { // one exiftool process, one command per file
		if i > 0 {
			args = append(args, "-execute")
		}
		args = append(args, "-XMP-xmpMM:PreservedFileName="+originals[files[i]], t)
	}
	_, err = exiftool(append(args, "-common_args", "-overwrite_original", "-q")...)
	return err
}

// metaTargets maps files to what exiftool writes: the file itself, or a RAW's sidecar (seeded when missing).
func metaTargets(files []string) ([]string, error) {
	var targets, newSidecars []string
	for _, f := range files {
		if !isRaw(f) {
			targets = append(targets, f)
			continue
		}
		sc := sidecarFor(f)
		if _, err := os.Stat(sc); err != nil {
			newSidecars = append(newSidecars, f)
		}
		targets = append(targets, sc)
	}
	if len(newSidecars) > 0 {
		// Seed sidecars from the RAW's own metadata, named IMG.xmp like Lightroom.
		if _, err := exiftool(append([]string{"-q", "-o", "%d%f.xmp"}, newSidecars...)...); err != nil {
			return nil, fmt.Errorf("creating XMP sidecars: %w", err)
		}
	}
	return targets, nil
}

// WriteRoll applies metadata to every frame of a roll, converted exports included.
func (l *Library) WriteRoll(dir string, m Meta) error {
	r := l.Roll(dir)
	if r == nil {
		return errors.New("roll not found")
	}
	var files []string
	for _, p := range slices.Concat(r.Frames, r.Exports) {
		files = append(files, filepath.FromSlash(p.SourceFile))
	}
	if err := writeMeta(files, m, r.Films); err != nil {
		return err
	}
	return l.rescanDir(dir)
}

// fileKeys identifies files already in the library by name and size, like Lightroom's duplicate check.
func (l *Library) fileKeys() map[string]bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	keys := map[string]bool{}
	for _, p := range l.photos {
		keys[fileKey(path.Base(p.SourceFile), p.FileSize)] = true
		if p.PreservedFileName != "" {
			keys[fileKey(string(p.PreservedFileName), p.FileSize)] = true
		}
	}
	return keys
}

func fileKey(name string, size int64) string {
	return fmt.Sprintf("%s|%d", strings.ToLower(name), size)
}

// Gear lists makes, models and lenses already used, for form suggestions.
func (l *Library) Gear() map[string][]string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	g := map[string][]string{"makes": {}, "models": {}, "lenses": {}}
	for _, p := range l.photos {
		g["makes"] = addUniq(g["makes"], string(p.Make))
		g["models"] = addUniq(g["models"], string(p.Model))
		g["lenses"] = addUniq(g["lenses"], string(p.LensModel))
	}
	for _, v := range g {
		sort.Strings(v)
	}
	return g
}
