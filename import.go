package main

// Lightroom-style import: pick a folder, card or lab zip, choose frames, and file them into
// the library using a folder-structure template, tagging them on the way in.

import (
	"archive/zip"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

type ImportFile struct {
	Rel      string // relative to the source root, forward slashes
	Size     int64
	Date     string // file date, YYYY-MM-DD
	Dup      bool   // same name and size already in the library
	Group    string // sub-folder inside the source, "" for the top level
	Detected bool   `json:",omitempty"` // grouped by its name rather than its folder
}

type ImportSource struct {
	Source string // folder or .zip the user picked
	Root   string // folder the files are in (a zip is extracted to staging)
	Zip    bool
	Name   string // suggested roll name
	Files  []ImportFile
	Groups []ImportGroup // sub-folders, each a candidate roll
	Rolls  int           // rolls found in the file names
}

// ImportGroup describes one sub-folder of a source, with suggestions for naming it as a roll.
type ImportGroup struct {
	Group    string // Rel path of the sub-folder, "" for the top level
	Count    int
	Folder   string // the folder's own name
	Lab      string `json:",omitempty"` // "Order B001738, roll 1" when the file names say so
	Detected bool   // this roll came from the file names, not a sub-folder
	Name     string // tidied name, e.g. "Roll 1 - Portra 400" -> "Portra 400"
	Film     string // best film database match for the folder name, if any
	ISO      string
	Date     string // earliest file date
}

func (a *App) stagingDir() string { return filepath.Join(a.DataDir, "staging") }

// OpenSource lists importable images in a folder or zip.
func (a *App) OpenSource(src string, subfolders, detect bool) (*ImportSource, error) {
	src = filepath.Clean(filepath.FromSlash(src))
	st, err := os.Stat(src)
	if err != nil {
		return nil, err
	}
	out := &ImportSource{Source: filepath.ToSlash(src), Root: src, Name: strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))}
	if !st.IsDir() {
		if ext(src) != "zip" {
			return nil, errors.New("pick a folder or a .zip file")
		}
		sum := sha1.Sum(fmt.Appendf(nil, "%s|%d|%d", src, st.Size(), st.ModTime().UnixNano()))
		out.Root = filepath.Join(a.stagingDir(), "zip-"+hex.EncodeToString(sum[:8]))
		if err := extractZip(src, out.Root); err != nil {
			return nil, err
		}
		out.Zip, subfolders = true, true
	} else {
		out.Name = filepath.Base(src)
	}
	keys := a.lib.fileKeys()
	for k := range a.importLog() {
		keys[k] = true
	}
	err = filepath.WalkDir(out.Root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entries are skipped, not fatal
		}
		name := d.Name()
		if d.IsDir() {
			if p != out.Root && (!subfolders || skipName(name) || a.isExport(name)) {
				return filepath.SkipDir
			}
			return nil
		}
		if skipName(name) || !isImage(name) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(out.Root, p)
		rel = filepath.ToSlash(rel)
		group := path.Dir(rel)
		if group == "." {
			group = ""
		}
		out.Files = append(out.Files, ImportFile{Rel: rel, Size: info.Size(), Date: info.ModTime().Format("2006-01-02"),
			Dup: keys[fileKey(name, info.Size())], Group: group})
		return nil
	})
	slices.SortFunc(out.Files, func(x, y ImportFile) int {
		if naturalLess(x.Rel, y.Rel) {
			return -1
		}
		return 1
	})
	if detect {
		out.Rolls = splitByName(out.Files)
	}
	out.Groups = a.groupsOf(out)
	return out, err
}

// splitByName re-groups each folder's files by the roll their names belong to, and returns how many
// rolls that found. Lab downloads arrive as one folder holding several rolls.
func splitByName(files []ImportFile) int {
	byFolder := map[string][]int{}
	for i, f := range files {
		byFolder[f.Group] = append(byFolder[f.Group], i)
	}
	rolls := 0
	for folder, idx := range byFolder {
		names := make([]string, len(idx))
		for i, fi := range idx {
			names[i] = path.Base(files[fi].Rel)
		}
		keys := rollKeys(names)
		if keys == nil {
			continue
		}
		for i, fi := range idx {
			files[fi].Group = path.Join(folder, keys[i])
			files[fi].Detected = true
		}
		rolls += distinct(keys)
	}
	return rolls
}

// groupsOf summarises a source's sub-folders and suggests a tidy roll name and film for each.
func (a *App) groupsOf(src *ImportSource) []ImportGroup {
	lines, _, _ := a.db.Lines()
	index := map[string]int{}
	var groups []ImportGroup
	for _, f := range src.Files {
		i, ok := index[f.Group]
		if !ok {
			i = len(groups)
			index[f.Group] = i
			groups = append(groups, ImportGroup{Group: f.Group, Date: f.Date})
		}
		groups[i].Count++
		groups[i].Date = min(groups[i].Date, f.Date)
	}
	for i := range groups {
		g := &groups[i]
		g.Folder = path.Base("/" + g.Group)
		if g.Group == "" {
			g.Folder = src.Name
		}
		g.Name = tidyName(g.Folder)
		filmFrom := g.Folder
		// The folder a lab download lands in ("OneDrive_2025-12-09") isn't a roll name: use the order and roll instead.
		first := firstIn(src.Files, g.Group)
		g.Detected = first.Detected
		if lab, ok := parseLabName(path.Base(first.Rel)); ok {
			filmFrom = path.Base("/" + path.Dir("/"+g.Group)) // an order-and-roll key says nothing about the film
			g.Lab = labLabel(lab)
			parent := path.Base("/" + path.Dir("/"+g.Group))
			if g.Detected || genericFolder.MatchString(g.Folder) || genericFolder.MatchString(parent) {
				g.Name = fmt.Sprintf("%s roll %d", lab.Order, lab.Roll)
			}
		}
		if d := folderDate(g.Folder + " " + path.Base("/"+path.Dir("/"+g.Group)) + " " + src.Name); d != "" {
			g.Date = d // a date in the folder name beats the file's modification time
		}
		g.Film = guessFilm(lines, filmFrom)
		g.ISO = guessISO(g.Film)
		if g.ISO == "" {
			g.ISO = guessISO(filmFrom)
		}
	}
	return groups
}

var rollPrefix = regexp.MustCompile(`(?i)^(?:(?:roll|film|order|job|scans?|negs?|negatives)\s*#?\s*\d+|#\d+|\d{1,3})(?:\s*[-_:.)]\s*|\s+)`)

// tidyName turns lab folder names into something nicer to browse: "roll_03_-_Portra_400" -> "Portra 400".
func tidyName(folder string) string {
	s := strings.Join(strings.Fields(strings.NewReplacer("_", " ", "+", " ").Replace(folder)), " ")
	if t := strings.TrimSpace(strings.Trim(rollPrefix.ReplaceAllString(s, ""), "-_. ")); t != "" {
		return t
	}
	return s
}

var filmStopWords = map[string]bool{"roll": true, "film": true, "films": true, "scan": true, "scans": true, "order": true, "the": true, "and": true,
	"lab": true, "hi": true, "res": true, "low": true, "high": true, "jpg": true, "jpeg": true, "tif": true, "tiff": true, "neg": true, "negs": true, "negatives": true, "of": true}

// guessFilm finds the film a folder name most likely refers to ("Roll 2 - HP5" -> "Ilford HP5").
// It needs a distinctive word, a word plus a speed, or two words to match, so "Beach day" guesses nothing.
func guessFilm(lines []string, folder string) string {
	var words []string
	for _, w := range nameTokens(folder) {
		if !filmStopWords[w] {
			words = append(words, w)
		}
	}
	best, bestScore := "", 0
	for i := 1; i < len(lines); i++ {
		fields := strings.Split(lines[i], ";")
		if len(fields) <= colName {
			continue
		}
		name := strings.TrimSpace(fields[colName])
		ft := nameTokens(name)
		alpha, digit, mixed := 0, 0, false
		for _, w := range words {
			if !slices.Contains(ft, w) {
				continue
			}
			switch {
			case isDigits(w):
				digit++
			case strings.ContainsAny(w, "0123456789"):
				if len(w) < 3 {
					continue // "r2" from a roll key is not a film name
				}
				alpha++
				mixed = true
			case len(w) >= 3:
				alpha++
			}
		}
		if alpha == 0 || (digit == 0 && alpha < 2 && !mixed) {
			continue
		}
		// Prefer names that are mostly explained by the folder: "Ilford HP5" beats "Ilford HP5 PLUS 400 (black box)".
		score := alpha*10 + digit*6 - (len(ft) - alpha - digit)
		if score > bestScore {
			best, bestScore = name, score
		}
	}
	return best
}

func nameTokens(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

func isDigits(s string) bool {
	return s != "" && strings.Trim(s, "0123456789") == ""
}

func firstIn(files []ImportFile, group string) ImportFile {
	for _, f := range files {
		if f.Group == group {
			return f
		}
	}
	return ImportFile{}
}

func (a *App) isExport(name string) bool {
	return slices.ContainsFunc(a.settings.Get().ExportDirs, func(e string) bool { return strings.EqualFold(e, name) })
}

// skipName ignores macOS/Windows junk that labs' zips are full of.
func skipName(name string) bool {
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "__MACOSX") || strings.EqualFold(name, "Thumbs.db") || strings.HasPrefix(name, "._")
}

const maxZipBytes = 64 << 30

// extractZip unpacks images and sidecars only, refusing paths that escape dest ("zip slip").
func extractZip(zipPath, dest string) error {
	done := filepath.Join(dest, ".complete")
	if _, err := os.Stat(done); err == nil {
		return nil
	}
	os.RemoveAll(dest)
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("can't open zip: %w", err)
	}
	defer zr.Close()
	var total uint64
	for _, f := range zr.File {
		name := path.Clean("/" + strings.ReplaceAll(f.Name, `\`, "/"))[1:]
		if f.FileInfo().IsDir() || name == "" || slices.ContainsFunc(strings.Split(name, "/"), skipName) || !(isImage(name) || ext(name) == "xmp") {
			continue
		}
		if total += f.UncompressedSize64; total > maxZipBytes {
			return errors.New("zip is larger than 64 GB uncompressed")
		}
		to := filepath.Join(dest, filepath.FromSlash(name))
		if rel, err := filepath.Rel(dest, to); err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		if err := writeZipEntry(f, to); err != nil {
			return err
		}
	}
	return os.WriteFile(done, nil, 0o644)
}

func writeZipEntry(f *zip.File, to string) error {
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.Create(to)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, io.LimitReader(rc, int64(f.UncompressedSize64)))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil && !f.Modified.IsZero() {
		os.Chtimes(to, f.Modified, f.Modified)
	}
	return err
}

type ImportRequest struct {
	Source     string
	Subfolders bool
	Files      []string // Rel paths to import; empty imports everything
	Mode       string   // copy | move | add
	Dest       string   // library root
	Structure  string   // folder template under Dest
	Rename     string   // file name template, "" keeps names
	Split      bool     // one roll per sub-folder of the source
	Detect     bool     // find rolls in the file names
	Name       string
	Meta
	Rolls []RollOverride // per-roll name and metadata when splitting
	After string         // app to open the new rolls in
}

// RollOverride names one sub-folder's roll; its non-empty metadata wins over the request's.
type RollOverride struct {
	Group, Name string
	Meta
}

type rollPlan struct {
	Name, Dir string // Dir is absolute
	Files     []ImportFile
	Meta      Meta
}

// merged overlays non-empty override fields on base.
func merged(base, over Meta) Meta {
	for _, kv := range []struct {
		dst *string
		src string
	}{{&base.Film, over.Film}, {&base.ISO, over.ISO}, {&base.Make, over.Make},
		{&base.Model, over.Model}, {&base.Lens, over.Lens}, {&base.Date, over.Date}} {
		if strings.TrimSpace(kv.src) != "" {
			*kv.dst = kv.src
		}
	}
	return base
}

var tokenRe = regexp.MustCompile(`\{(\w+)\}`)

func render(tpl string, vals map[string]string) string {
	return tokenRe.ReplaceAllStringFunc(tpl, func(t string) string {
		if v, ok := vals[t[1:len(t)-1]]; ok {
			return v
		}
		return t
	})
}

var badChars = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]+`)

// safeSegment makes one path segment valid on Windows, macOS and Linux.
func safeSegment(s string) string {
	s = strings.Join(strings.Fields(badChars.ReplaceAllString(s, "-")), " ")
	s = strings.Trim(s, " .-")
	if s == "." || s == ".." {
		return ""
	}
	return s
}

// tokens are the values a structure or rename template can use.
func tokens(req ImportRequest, name, group, date string) map[string]string {
	if req.Date != "" {
		date = req.Date
	}
	vals := map[string]string{
		"name": name, "film": req.Film, "make": req.Make, "model": req.Model, "lens": req.Lens, "iso": req.ISO,
		"camera": strings.TrimSpace(req.Make + " " + req.Model), "date": date, "group": strings.TrimPrefix(path.Base("/"+group), "/"),
		"source": strings.TrimSuffix(path.Base(filepath.ToSlash(req.Source)), path.Ext(req.Source)),
		"yyyy":   "", "yy": "", "mm": "", "dd": "",
	}
	if len(date) == 10 {
		vals["yyyy"], vals["yy"], vals["mm"], vals["dd"] = date[:4], date[2:4], date[5:7], date[8:10]
	}
	for k, v := range vals {
		vals[k] = badChars.ReplaceAllString(v, "-") // a "/" in a film name must not create folders
	}
	return vals
}

// folderFor renders the structure template into a relative folder path.
func folderFor(tpl string, vals map[string]string) (string, error) {
	var segs []string
	for _, s := range strings.Split(strings.ReplaceAll(render(tpl, vals), `\`, "/"), "/") {
		if s = safeSegment(s); s != "" {
			segs = append(segs, s)
		}
	}
	if len(segs) == 0 {
		return "", errors.New("the folder template gives an empty folder name — add a roll name")
	}
	return filepath.Join(segs...), nil
}

// fileName renders the rename template for the i-th of n files.
func fileName(tpl string, vals map[string]string, orig string, i, n int) string {
	if strings.TrimSpace(tpl) == "" {
		return orig
	}
	e := filepath.Ext(orig)
	v := map[string]string{"original": strings.TrimSuffix(orig, e), "seq": fmt.Sprintf("%0*d", max(2, len(strconv.Itoa(n))), i+1)}
	for k, x := range vals {
		v[k] = x
	}
	if s := safeSegment(render(tpl, v)); s != "" {
		return s + strings.ToLower(e)
	}
	return orig
}

// PlanImport validates a request and works out which files go into which roll folder.
func (a *App) PlanImport(req ImportRequest, src *ImportSource) ([]rollPlan, error) {
	s := a.settings.Get()
	if !slices.Contains([]string{"copy", "move", "add"}, req.Mode) {
		return nil, errors.New("mode must be copy, move or add")
	}
	if _, err := metaArgs(req.Meta, nil); err != nil {
		return nil, err
	}
	for _, o := range req.Rolls {
		if _, err := metaArgs(o.Meta, nil); err != nil {
			return nil, fmt.Errorf("%s: %w", o.Name, err)
		}
	}
	files := src.Files
	if len(req.Files) > 0 {
		want := map[string]bool{}
		for _, f := range req.Files {
			want[f] = true
		}
		files = slices.DeleteFunc(slices.Clone(files), func(f ImportFile) bool { return !want[f.Rel] })
	}
	if len(files) == 0 {
		return nil, errors.New("no photos selected")
	}

	if req.Mode == "add" {
		if src.Zip {
			return nil, errors.New("a zip can't be added in place — copy it into a library")
		}
		index := map[string]int{}
		var plans []rollPlan
		for _, f := range files {
			abs := filepath.Join(src.Root, filepath.FromSlash(f.Rel))
			if !inAnyLibrary(abs, s.Libraries) {
				return nil, errors.New("add in place only works for folders inside a library — use copy instead")
			}
			d := filepath.Dir(abs)
			i, ok := index[d]
			if !ok {
				i = len(plans)
				index[d] = i
				plans = append(plans, rollPlan{Name: filepath.Base(d), Dir: d, Meta: req.Meta})
			}
			plans[i].Files = append(plans[i].Files, f)
		}
		return plans, nil
	}

	if !slices.Contains(s.Libraries, req.Dest) {
		return nil, errors.New("choose a library folder to import into")
	}
	structure := req.Structure
	if strings.TrimSpace(structure) == "" {
		structure = "{name}"
	}
	groups := map[string][]ImportFile{"": files}
	order := []string{""}
	if req.Split {
		groups, order = map[string][]ImportFile{}, nil
		for _, f := range files {
			if _, ok := groups[f.Group]; !ok {
				order = append(order, f.Group)
			}
			groups[f.Group] = append(groups[f.Group], f)
		}
	}
	var plans []rollPlan
	seen := map[string]string{}
	for _, g := range order {
		gf := groups[g]
		name, meta := strings.TrimSpace(req.Name), req.Meta
		if req.Split {
			name = tidyName(path.Base("/" + g))
			if i := slices.IndexFunc(src.Groups, func(x ImportGroup) bool { return x.Group == g }); i >= 0 && src.Groups[i].Name != "" {
				name = src.Groups[i].Name // the suggestion the import page shows
			}
			if i := slices.IndexFunc(req.Rolls, func(o RollOverride) bool { return o.Group == g }); i >= 0 {
				if n := strings.TrimSpace(req.Rolls[i].Name); n != "" {
					name = n
				}
				meta = merged(meta, req.Rolls[i].Meta)
			}
		}
		if name == "" {
			name = src.Name
		}
		date := gf[0].Date
		for _, f := range gf {
			date = min(date, f.Date)
		}
		r := req
		r.Meta = meta
		rel, err := folderFor(structure, tokens(r, name, g, date))
		if err != nil {
			return nil, err
		}
		dir := filepath.Join(filepath.FromSlash(req.Dest), rel)
		if other, dup := seen[strings.ToLower(dir)]; dup {
			return nil, fmt.Errorf("the rolls %q and %q would go into the same folder — give them different names", other, name)
		}
		seen[strings.ToLower(dir)] = name
		plans = append(plans, rollPlan{Name: name, Dir: dir, Files: gf, Meta: meta})
	}
	return plans, nil
}

func inAnyLibrary(abs string, libs []string) bool {
	return slices.ContainsFunc(libs, func(l string) bool {
		rel, err := filepath.Rel(filepath.FromSlash(l), abs)
		return err == nil && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)
	})
}

var importMu sync.Mutex

// importLog remembers the name and size of every source file imported, because tagging changes the
// copy's size and a renamed copy has a new name. It's what lets a lab zip show as already imported.
var logMu sync.Mutex

func (a *App) importLog() map[string]bool {
	logMu.Lock()
	defer logMu.Unlock()
	keys := map[string]bool{}
	if b, err := os.ReadFile(filepath.Join(a.DataDir, "imported.json")); err == nil {
		var list []string
		if json.Unmarshal(b, &list) == nil {
			for _, k := range list {
				keys[k] = true
			}
		}
	}
	return keys
}

func (a *App) logImported(files []ImportFile) {
	keys := a.importLog()
	for _, f := range files {
		keys[fileKey(path.Base(f.Rel), f.Size)] = true
	}
	logMu.Lock()
	defer logMu.Unlock()
	b, _ := json.Marshal(slices.Sorted(maps.Keys(keys)))
	p := filepath.Join(a.DataDir, "imported.json")
	if os.WriteFile(p+".tmp", b, 0o644) == nil {
		os.Rename(p+".tmp", p)
	}
}

// Import starts an import in the background.
func (a *App) Import(req ImportRequest) error {
	src, err := a.OpenSource(req.Source, req.Subfolders, req.Detect)
	if err != nil {
		return err
	}
	plans, err := a.PlanImport(req, src)
	if err != nil {
		return err
	}
	if !importMu.TryLock() {
		return errors.New("an import is already running")
	}
	go func() {
		defer importMu.Unlock()
		a.runImport(req, src, plans)
	}()
	return nil
}

func (a *App) runImport(req ImportRequest, src *ImportSource, plans []rollPlan) error {
	total := 0
	for _, p := range plans {
		total += len(p.Files)
	}
	label := plans[0].Name
	if len(plans) > 1 {
		label = fmt.Sprintf("%d rolls", len(plans))
	}
	a.lib.setJob(func(j *Job) { *j = Job{Kind: "import", Message: "Importing " + label, Total: total, Running: true} })
	dirs, err := a.importFiles(req, src, plans)
	a.lib.setJob(func(j *Job) {
		j.Running, j.Dirs = false, dirs
		if err != nil {
			j.Message, j.Error = "Import failed", err.Error()
		} else {
			j.Message = "Imported " + label
		}
	})
	if err == nil && src.Zip {
		os.RemoveAll(src.Root)
	}
	if err == nil && req.After != "" && len(dirs) > 0 && a.Desktop {
		if oerr := a.OpenIn(req.After, dirs[0], nil); oerr != nil {
			logf("open after import: %v", oerr)
		}
	}
	return err
}

func (a *App) importFiles(req ImportRequest, src *ImportSource, plans []rollPlan) ([]string, error) {
	var dirs []string
	done := 0
	for _, pl := range plans {
		var written []string
		if req.Mode == "add" {
			for _, f := range pl.Files {
				written = append(written, filepath.Join(src.Root, filepath.FromSlash(f.Rel)))
			}
			done += len(pl.Files)
		} else {
			if err := os.MkdirAll(pl.Dir, 0o755); err != nil {
				return dirs, err
			}
			r := req
			r.Meta = pl.Meta
			vals := tokens(r, pl.Name, "", "")
			moved := map[string]bool{}
			renamed := map[string]string{}
			for i, f := range pl.Files {
				from := filepath.Join(src.Root, filepath.FromSlash(f.Rel))
				to := uniquePath(filepath.Join(pl.Dir, fileName(req.Rename, vals, filepath.Base(from), i, len(pl.Files))))
				move := req.Mode == "move" || src.Zip
				if err := transfer(from, to, move); err != nil {
					return dirs, err
				}
				// Sidecars travel with their RAW and follow its new name.
				for _, sc := range []string{from + ".xmp", strings.TrimSuffix(from, filepath.Ext(from)) + ".xmp"} {
					if _, err := os.Stat(sc); err != nil || moved[sc] {
						continue
					}
					scTo := strings.TrimSuffix(to, filepath.Ext(to)) + ".xmp"
					if strings.HasSuffix(strings.ToLower(sc), strings.ToLower(filepath.Ext(from))+".xmp") {
						scTo = to + ".xmp"
					}
					if err := transfer(sc, uniquePath(scTo), move); err != nil {
						return dirs, err
					}
					moved[sc] = true
				}
				if filepath.Base(to) != filepath.Base(from) {
					renamed[to] = filepath.Base(from)
				}
				written = append(written, to)
				done++
				a.lib.setJob(func(j *Job) { j.Done = done })
			}
			a.logImported(pl.Files)
			if err := preserveNames(renamed); err != nil {
				return dirs, err
			}
		}
		a.lib.setJob(func(j *Job) { j.Message = "Tagging " + pl.Name + "…" })
		if err := writeMeta(written, pl.Meta, nil); err != nil {
			return dirs, err
		}
		dir := filepath.ToSlash(pl.Dir)
		if req.Mode == "add" {
			// a converted/export folder belongs to its parent roll
			if a.isExport(filepath.Base(pl.Dir)) {
				dir = path.Dir(dir)
			}
		}
		if err := a.lib.rescanDir(dir); err != nil {
			return dirs, err
		}
		dirs = append(dirs, dir)
	}
	return dirs, nil
}

// transfer copies or moves a file without ever overwriting.
func transfer(from, to string, move bool) error {
	if move {
		if _, err := os.Stat(to); os.IsNotExist(err) && os.Rename(from, to) == nil {
			return nil
		}
	}
	if err := copyFile(from, to); err != nil {
		return err
	}
	if move {
		return os.Remove(from)
	}
	return nil
}

// uniquePath appends " (2)", " (3)"… until the name is free.
func uniquePath(p string) string {
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return p
	}
	e := filepath.Ext(p)
	stem := strings.TrimSuffix(p, e)
	for i := 2; ; i++ {
		c := fmt.Sprintf("%s (%d)%s", stem, i, e)
		if _, err := os.Stat(c); os.IsNotExist(err) {
			return c
		}
	}
}

// copyFile never overwrites and never leaves half-written files behind.
func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	if _, err := os.Stat(to); err == nil {
		return fmt.Errorf("%s already exists", to)
	}
	tmp := to + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, to)
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if st, err := in.Stat(); err == nil {
		os.Chtimes(to, st.ModTime(), st.ModTime())
	}
	return nil
}

// ---- hot folder ----

// HotFolderLoop imports lab zips and folders dropped into the hot folder, then files the originals under Imported/.
func (a *App) HotFolderLoop() {
	failed := map[string]bool{}
	for {
		time.Sleep(time.Minute)
		a.checkHotFolder(failed)
	}
}

func (a *App) checkHotFolder(failed map[string]bool) {
	s := a.settings.Get()
	hf := s.HotFolder
	if !hf.Enabled || hf.Path == "" || s.ImportTo == "" {
		return
	}
	entries, err := os.ReadDir(filepath.FromSlash(hf.Path))
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		full := filepath.Join(filepath.FromSlash(hf.Path), name)
		if skipName(name) || strings.EqualFold(name, "Imported") || failed[full] || !(e.IsDir() || ext(name) == "zip") {
			continue
		}
		if time.Since(latestMod(full)) < 2*time.Minute { // still downloading or copying
			continue
		}
		src, err := a.OpenSource(full, true, true)
		if err != nil || len(src.Files) == 0 {
			failed[full] = true
			continue
		}
		req := ImportRequest{Source: full, Subfolders: true, Detect: true, Split: src.Rolls > 1, Mode: "copy", Dest: s.ImportTo, Structure: s.Structure, Rename: s.Rename, Name: src.Name}
		plans, err := a.PlanImport(req, src)
		if err == nil && importMu.TryLock() {
			err = a.runImport(req, src, plans)
			importMu.Unlock()
		} else if err == nil {
			return // another import is running; try next minute
		}
		if err != nil {
			failed[full] = true
			a.lib.setStatus("Hot folder: " + name + ": " + err.Error())
			continue
		}
		done := filepath.Join(filepath.FromSlash(hf.Path), "Imported")
		os.MkdirAll(done, 0o755)
		if err := os.Rename(full, uniquePath(filepath.Join(done, name))); err != nil {
			failed[full] = true // don't import it again this session
		}
		return // one per minute keeps the NAS responsive
	}
}

func latestMod(p string) time.Time {
	var t time.Time
	filepath.WalkDir(p, func(_ string, d fs.DirEntry, err error) error {
		if err == nil {
			if info, err := d.Info(); err == nil && info.ModTime().After(t) {
				t = info.ModTime()
			}
		}
		return nil
	})
	return t
}

// CleanStaging removes leftover zip extractions and uploads older than a week.
func (a *App) CleanStaging() {
	entries, _ := os.ReadDir(a.stagingDir())
	for _, e := range entries {
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > 7*24*time.Hour {
			os.RemoveAll(filepath.Join(a.stagingDir(), e.Name()))
		}
	}
}
