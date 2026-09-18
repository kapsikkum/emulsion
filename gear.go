package main

// Camera gear: a git clone of the camera gear database, plus whatever you add yourself.
// Your own gear lives in the data folder, so updating the database never touches it.

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

const gearUpstream = "https://github.com/kapsikkum/camera-gear-database"

type GearItem struct {
	Slug, Kind string // kind: body or lens
	Name       string
	Brand      string `json:",omitempty"`
	Model      string `json:",omitempty"`
	Mount      string `json:",omitempty"`
	Format     string `json:",omitempty"` // film it takes: 35mm, 120, APS
	Type       string `json:",omitempty"` // SLR or DSLR
	Focal      string `json:",omitempty"`
	Aperture   string `json:",omitempty"`
	Introduced string `json:",omitempty"`
	Filter     string `json:",omitempty"`
	Weight     string `json:",omitempty"`
	Image      string `json:",omitempty"` // URL the UI can load
	Wikipedia  string `json:",omitempty"`
	Custom     bool   `json:",omitempty"`
	Rolls      int    `json:",omitempty"` // rolls in the library shot with it
}

type GearDB struct {
	Dir        string // the clone
	customFile string
	imageDir   string
	mu         sync.Mutex
	status     string
	updated    time.Time

	cacheMu sync.Mutex
	cache   []GearItem
	cacheAt time.Time
}

func NewGearDB(dataDir string) *GearDB {
	return &GearDB{
		Dir:        filepath.Join(dataDir, "geardb"),
		customFile: filepath.Join(dataDir, "gear.json"),
		imageDir:   filepath.Join(dataDir, "gear-images"),
	}
}

func (g *GearDB) Status() (string, time.Time) { return g.status, g.updated }

func (g *GearDB) setStatus(s string) {
	g.status = s
	logf("gear db: %s", s)
}

// Sync clones the gear database on first run, then pulls.
func (g *GearDB) Sync() {
	g.mu.Lock()
	defer g.mu.Unlock()
	db := &FilmDB{Dir: g.Dir} // same git plumbing
	if _, err := os.Stat(filepath.Join(g.Dir, "data")); err != nil {
		g.setStatus("Downloading gear database…")
		tmp := g.Dir + ".tmp"
		os.RemoveAll(tmp)
		os.MkdirAll(filepath.Dir(g.Dir), 0o755)
		if out, err := db.git(filepath.Dir(g.Dir), "clone", "--depth", "1", "--config", "core.autocrlf=false", gearUpstream, filepath.Base(tmp)); err != nil {
			g.setStatus("Download failed: " + firstLine(out, err))
			return
		}
		os.RemoveAll(g.Dir)
		if err := os.Rename(tmp, g.Dir); err != nil {
			g.setStatus("Download failed: " + err.Error())
			return
		}
	} else {
		// Nothing in the clone is yours, so take upstream's word for it. A shallow pull can't
		// merge at all once upstream's history has moved on: the two sides share no commit.
		for _, args := range [][]string{{"fetch", "--depth", "1", "origin", "HEAD"}, {"reset", "--hard", "FETCH_HEAD"}} {
			if out, err := db.git(g.Dir, args...); err != nil {
				g.setStatus("Update failed: " + firstLine(out, err))
				return
			}
		}
		// Replaced photos pile up as unreachable objects, so the clone would grow forever.
		db.git(g.Dir, "reflog", "expire", "--expire=now", "--all")
		db.git(g.Dir, "gc", "--prune=now", "--quiet")
	}
	g.invalidate()
	g.updated = time.Now()
	g.setStatus("Up to date")
}

func (g *GearDB) invalidate() {
	g.cacheMu.Lock()
	g.cache, g.cacheAt = nil, time.Time{}
	g.cacheMu.Unlock()
}

// Items returns the database and your own gear, yours winning where the slugs match.
func (g *GearDB) Items() []GearItem {
	g.cacheMu.Lock()
	defer g.cacheMu.Unlock()
	if g.cache != nil && time.Since(g.cacheAt) < time.Minute {
		return g.cache
	}
	items := g.fromRepo()
	index := map[string]int{}
	for i, it := range items {
		index[it.Slug] = i
	}
	for _, own := range g.custom() {
		own.Custom = true
		if own.Image == "" {
			own.Image = g.customImage(own.Slug)
		}
		if i, ok := index[own.Slug]; ok {
			items[i] = own
			continue
		}
		index[own.Slug] = len(items)
		items = append(items, own)
	}
	slices.SortFunc(items, func(a, b GearItem) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
	g.cache, g.cacheAt = items, time.Now()
	return items
}

// fromRepo reads every brand's bodies.csv and lenses.csv out of the clone.
func (g *GearDB) fromRepo() []GearItem {
	var items []GearItem
	root := filepath.Join(g.Dir, "data")
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		kind := map[string]string{"bodies.csv": "body", "lenses.csv": "lens"}[strings.ToLower(d.Name())]
		if kind == "" {
			return nil
		}
		f, err := os.Open(p)
		if err != nil {
			return nil
		}
		defer f.Close()
		rows, err := csv.NewReader(f).ReadAll()
		if err != nil || len(rows) < 2 {
			return nil
		}
		col := map[string]int{}
		for i, h := range rows[0] {
			col[h] = i
		}
		at := func(row []string, name string) string {
			if i, ok := col[name]; ok && i < len(row) {
				return strings.TrimSpace(row[i])
			}
			return ""
		}
		for _, row := range rows[1:] {
			it := GearItem{
				Slug: at(row, "slug"), Kind: kind, Name: at(row, "name"), Mount: at(row, "mount"), Type: at(row, "type"),
				Focal: at(row, "focal_length_mm"), Aperture: at(row, "max_aperture"), Introduced: at(row, "introduced"),
				Filter: at(row, "filter_mm"), Weight: at(row, "weight_g"), Wikipedia: at(row, "wikipedia"),
				Format: at(row, "film_format"),
			}
			if it.Slug == "" || it.Name == "" {
				continue
			}
			it.Brand, it.Model = splitBrand(it.Name)
			if b := at(row, "brand"); b != "" {
				it.Brand = b // Nikkor lenses are Nikon's, whatever the name says
			}
			if img := at(row, "image"); img != "" {
				it.Image = "/geardb/" + filepath.ToSlash(img)
			}
			items = append(items, it)
		}
		return nil
	})
	return items
}

// splitBrand turns "Canon AE-1" into the Make and Model a photo's EXIF wants.
func splitBrand(name string) (string, string) {
	brand, model, ok := strings.Cut(strings.TrimSpace(name), " ")
	if !ok {
		return name, name
	}
	return brand, model
}

func (g *GearDB) custom() []GearItem {
	b, err := os.ReadFile(g.customFile)
	if err != nil {
		return nil
	}
	var items []GearItem
	json.Unmarshal(b, &items)
	return items
}

func (g *GearDB) customImage(slug string) string {
	for _, ext := range []string{".jpg", ".png", ".webp"} {
		if _, err := os.Stat(filepath.Join(g.imageDir, slug+ext)); err == nil {
			return "/api/gear/photo?slug=" + slug
		}
	}
	return ""
}

var gearMu sync.Mutex

// Save adds or updates one of your own gear entries. Editing a database entry keeps its slug,
// so your version replaces it everywhere without losing the link.
func (g *GearDB) Save(it GearItem) (GearItem, error) {
	it.Name = strings.Join(strings.Fields(it.Name), " ")
	if it.Name == "" {
		return it, errors.New("give it a name")
	}
	if it.Kind != "body" && it.Kind != "lens" {
		return it, errors.New("gear must be a body or a lens")
	}
	if it.Brand == "" || it.Model == "" {
		it.Brand, it.Model = splitBrand(it.Name)
	}
	gearMu.Lock()
	defer gearMu.Unlock()
	own := g.custom()
	if it.Slug == "" {
		base := slugify(it.Name)
		if base == "" {
			return it, errors.New("give it a name")
		}
		taken := map[string]bool{}
		for _, x := range g.Items() {
			taken[x.Slug] = true
		}
		it.Slug = base
		for i := 2; taken[it.Slug]; i++ {
			it.Slug = fmt.Sprintf("%s-%d", base, i)
		}
	}
	it.Custom = true
	it.Rolls = 0
	if i := slices.IndexFunc(own, func(x GearItem) bool { return x.Slug == it.Slug }); i >= 0 {
		own[i] = it
	} else {
		own = append(own, it)
	}
	return it, g.writeCustom(own)
}

// Forget removes one of your own entries; a database entry you had edited goes back to its original.
func (g *GearDB) Forget(slug string) error {
	gearMu.Lock()
	defer gearMu.Unlock()
	own := g.custom()
	rest := slices.DeleteFunc(own, func(x GearItem) bool { return x.Slug == slug })
	if len(rest) == len(own) {
		return errors.New("that isn't your own gear")
	}
	for _, ext := range []string{".jpg", ".png", ".webp"} {
		os.Remove(filepath.Join(g.imageDir, slug+ext))
	}
	return g.writeCustom(rest)
}

func (g *GearDB) writeCustom(items []GearItem) error {
	b, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(g.customFile+".tmp", b, 0o644); err != nil {
		return err
	}
	if err := os.Rename(g.customFile+".tmp", g.customFile); err != nil {
		return err
	}
	g.invalidate()
	return nil
}

// SaveImage stores a photo for one of your own gear entries.
func (g *GearDB) SaveImage(slug, ext string, data []byte) error {
	if !slices.ContainsFunc(g.Items(), func(x GearItem) bool { return x.Slug == slug }) {
		return errors.New("unknown gear")
	}
	if !slices.Contains([]string{".jpg", ".jpeg", ".png", ".webp"}, strings.ToLower(ext)) {
		return errors.New("use a JPEG, PNG or WebP image")
	}
	if err := os.MkdirAll(g.imageDir, 0o755); err != nil {
		return err
	}
	for _, e := range []string{".jpg", ".png", ".webp"} {
		os.Remove(filepath.Join(g.imageDir, slug+e))
	}
	if strings.EqualFold(ext, ".jpeg") {
		ext = ".jpg"
	}
	if err := os.WriteFile(filepath.Join(g.imageDir, slug+strings.ToLower(ext)), data, 0o644); err != nil {
		return err
	}
	g.invalidate()
	return nil
}

func (g *GearDB) ImagePath(slug string) string {
	for _, ext := range []string{".jpg", ".png", ".webp"} {
		p := filepath.Join(g.imageDir, slug+ext)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// RepoImage resolves a /geardb/… URL to a file inside the clone.
func (g *GearDB) RepoImage(rel string) (string, error) {
	clean := path.Clean("/" + rel)[1:]
	p := filepath.Join(g.Dir, filepath.FromSlash(clean))
	if r, err := filepath.Rel(g.Dir, p); err != nil || strings.HasPrefix(r, "..") {
		return "", errors.New("not found")
	}
	return p, nil
}

func slugify(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// GearQuery is what the gear page and the pickers ask for. Empty fields mean anything.
type GearQuery struct{ Kind, Q, Brand, Mount, Format string }

// mountNames splits a row's mounts and drops the "lens mount" Wikidata suffixes.
func mountNames(mount string) []string {
	var out []string
	for _, part := range strings.Split(mount, ";") {
		if p := strings.TrimSuffix(strings.TrimSpace(part), " lens mount"); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Facets lists the brands, mounts and film formats one kind of gear actually has, for the filters.
func (g *GearDB) Facets(kind string) map[string][]string {
	seen := map[string]map[string]bool{"brands": {}, "mounts": {}, "formats": {}}
	for _, it := range g.Items() {
		if kind != "" && it.Kind != kind {
			continue
		}
		seen["brands"][it.Brand] = true
		seen["formats"][it.Format] = true
		for _, m := range mountNames(it.Mount) {
			seen["mounts"][m] = true
		}
	}
	out := map[string][]string{}
	for what, set := range seen {
		delete(set, "")
		names := slices.Collect(maps.Keys(set))
		slices.SortFunc(names, func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) })
		out[what] = names
	}
	return out
}

// Search ranks gear by how well it matches the words typed, gear you've used first.
func (g *GearDB) Search(q GearQuery, used map[string]int, limit int) []GearItem {
	words := strings.Fields(strings.ToLower(q.Q))
	same := func(a, b string) bool { return strings.EqualFold(a, b) }
	var out []GearItem
	for _, it := range g.Items() {
		if q.Kind != "" && it.Kind != q.Kind {
			continue
		}
		if q.Brand != "" && !same(it.Brand, q.Brand) {
			continue
		}
		if q.Format != "" && !same(it.Format, q.Format) {
			continue
		}
		if q.Mount != "" && !slices.ContainsFunc(mountNames(it.Mount), func(m string) bool { return same(m, q.Mount) }) {
			continue
		}
		hay := strings.ToLower(it.Name + " " + it.Mount + " " + it.Brand)
		if slices.ContainsFunc(words, func(w string) bool { return !strings.Contains(hay, w) }) {
			continue
		}
		it.Rolls = used[strings.ToLower(it.Name)]
		out = append(out, it)
	}
	slices.SortStableFunc(out, func(a, b GearItem) int {
		if a.Rolls != b.Rolls {
			return b.Rolls - a.Rolls
		}
		if a.Custom != b.Custom {
			if a.Custom {
				return -1
			}
			return 1
		}
		return nameScore(b.Name, words) - nameScore(a.Name, words)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}
