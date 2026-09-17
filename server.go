package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed web
var webFS embed.FS

type App struct {
	DataDir  string
	Desktop  bool // running with a native window
	settings *Store
	db       *FilmDB
	lib      *Library
	thumbs   *Thumbs
	updater  *Updater
	sessions Sessions
	loginMu  sync.Mutex

	remoteMu  sync.Mutex
	remote    *http.Server
	remoteErr string
}

func NewApp(dataDir string, desktop bool) *App {
	os.MkdirAll(dataDir, 0o755)
	settings := LoadSettings(dataDir)
	a := &App{
		DataDir:  dataDir,
		Desktop:  desktop,
		settings: settings,
		db:       &FilmDB{Dir: filepath.Join(dataDir, "filmdb")},
		lib:      NewLibrary(dataDir, func() []string { return settings.Get().ExportDirs }),
		thumbs:   NewThumbs(filepath.Join(dataDir, "thumbs")),
		updater:  NewUpdater(),
	}
	a.updater.beforeRestart = func() {
		a.remoteMu.Lock()
		defer a.remoteMu.Unlock()
		if a.remote != nil {
			a.remote.Close()
		}
	}
	return a
}

// Background: film DB sync on start and every UpdateHours; library scan on start.
func (a *App) Start() {
	go a.lib.Scan(a.settings.Get().Libraries)
	go a.CleanStaging()
	go a.HotFolderLoop()
	go CleanOld()
	go func() {
		time.Sleep(30 * time.Second) // don't slow down startup
		for {
			if !a.settings.Get().NoUpdateCheck {
				if info := a.updater.Check(); info.Available {
					logf("update available: %s", info.Latest)
				}
			}
			time.Sleep(24 * time.Hour)
		}
	}()
	go func() {
		for {
			if _, last := a.db.Status(); time.Since(last) >= time.Duration(max(1, a.settings.Get().UpdateHours))*time.Hour {
				a.db.Sync()
			}
			time.Sleep(10 * time.Minute)
		}
	}()
}

// ---- helpers ----

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func readJSON(r *http.Request, v any) error {
	return json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(v)
}

// ---- handler ----

// Handler builds the HTTP handler. local = the desktop window's loopback listener.
func (a *App) Handler(local bool) http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(webFS, "web")
	files := http.FileServerFS(static)
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		// Embedded files carry no modification time; without this, browsers keep old UI code after an update.
		w.Header().Set("Cache-Control", "no-cache")
		files.ServeHTTP(w, r)
	})

	mux.HandleFunc("POST /api/login", a.login)
	mux.HandleFunc("POST /api/logout", func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("emulsion"); err == nil {
			a.sessions.Delete(c.Value)
		}
		http.SetCookie(w, &http.Cookie{Name: "emulsion", Path: "/", MaxAge: -1})
		writeJSON(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) { a.state(w, r, local) })
	mux.HandleFunc("GET /api/rolls", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, a.lib.Rolls(false)) })
	mux.HandleFunc("GET /api/roll", a.roll)
	mux.HandleFunc("GET /api/gear", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, a.lib.Gear()) })
	mux.HandleFunc("POST /api/roll", a.rollSave)
	mux.HandleFunc("POST /api/rescan", func(w http.ResponseWriter, r *http.Request) {
		go a.lib.Scan(a.settings.Get().Libraries)
		writeJSON(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /api/import/scan", func(w http.ResponseWriter, r *http.Request) {
		src, err := a.OpenSource(r.FormValue("source"), r.FormValue("sub") == "1")
		if err != nil {
			fail(w, 400, err)
			return
		}
		writeJSON(w, src)
	})
	mux.HandleFunc("GET /api/import/thumb", a.importThumb)
	mux.HandleFunc("POST /api/import/plan", func(w http.ResponseWriter, r *http.Request) {
		var req ImportRequest
		if err := readJSON(r, &req); err != nil {
			fail(w, 400, err)
			return
		}
		src, err := a.OpenSource(req.Source, req.Subfolders)
		if err == nil {
			var plans []rollPlan
			if plans, err = a.PlanImport(req, src); err == nil {
				out := []map[string]any{}
				for _, p := range plans {
					out = append(out, map[string]any{"Name": p.Name, "Dir": filepath.ToSlash(p.Dir), "Count": len(p.Files), "Film": p.Meta.Film})
				}
				writeJSON(w, out)
				return
			}
		}
		fail(w, 400, err)
	})
	mux.HandleFunc("POST /api/import", func(w http.ResponseWriter, r *http.Request) {
		var req ImportRequest
		if err := readJSON(r, &req); err != nil {
			fail(w, 400, err)
			return
		}
		if !local {
			req.After = "" // apps open on the desktop, not for remote users
		}
		if err := a.Import(req); err != nil {
			fail(w, 400, err)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/upload", a.upload)
	mux.HandleFunc("GET /api/apps", func(w http.ResponseWriter, r *http.Request) {
		if !local {
			writeJSON(w, []AppInfo{})
			return
		}
		writeJSON(w, a.Apps())
	})
	mux.HandleFunc("POST /api/open", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			App, Dir string
			Files    []string
		}
		if err := readJSON(r, &req); err != nil {
			fail(w, 400, err)
			return
		}
		if !local {
			fail(w, 403, errors.New("apps can only be opened from the desktop app"))
			return
		}
		if req.App == "data" {
			if err := reveal(a.DataDir, nil); err != nil {
				fail(w, 500, err)
				return
			}
			writeJSON(w, map[string]bool{"ok": true})
			return
		}
		if a.lib.Roll(req.Dir) == nil {
			fail(w, 404, errors.New("roll not found"))
			return
		}
		for _, f := range req.Files {
			if _, ok := a.lib.Photo(f); !ok {
				fail(w, 404, errors.New("photo not found"))
				return
			}
		}
		if err := a.OpenIn(req.App, req.Dir, req.Files); err != nil {
			fail(w, 400, err)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /api/films", a.films)
	mux.HandleFunc("GET /api/film", a.film)
	mux.HandleFunc("POST /api/film", a.filmSave)
	mux.HandleFunc("POST /api/filmdb/update", func(w http.ResponseWriter, r *http.Request) {
		go a.db.Sync()
		writeJSON(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/filmdb/revert", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Hash string }
		if err := readJSON(r, &req); err != nil {
			fail(w, 400, err)
			return
		}
		if err := a.db.Revert(req.Hash); err != nil {
			fail(w, 400, err)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /api/update", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, a.updater.Info()) })
	mux.HandleFunc("POST /api/update/check", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, a.updater.Check()) })
	mux.HandleFunc("POST /api/update/install", func(w http.ResponseWriter, r *http.Request) {
		if err := a.updater.Start(); err != nil {
			fail(w, 400, err)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /api/fs", a.browse)
	mux.HandleFunc("POST /api/fs/mkdir", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Parent, Name string }
		if err := readJSON(r, &req); err != nil {
			fail(w, 400, err)
			return
		}
		name := strings.TrimSpace(req.Name)
		if name == "" || safeSegment(name) != name {
			fail(w, 400, errors.New(`folder names can't contain < > : " / \ | ? * or start or end with a dot`))
			return
		}
		parent := filepath.Clean(filepath.FromSlash(req.Parent))
		if st, err := os.Stat(parent); err != nil || !st.IsDir() || !filepath.IsAbs(parent) {
			fail(w, 400, errors.New("open a folder first"))
			return
		}
		p := filepath.Join(parent, name)
		if err := os.Mkdir(p, 0o755); err != nil {
			fail(w, 400, err)
			return
		}
		writeJSON(w, map[string]string{"Path": filepath.ToSlash(p)})
	})
	mux.HandleFunc("POST /api/settings", func(w http.ResponseWriter, r *http.Request) { a.saveSettings(w, r, local) })
	mux.HandleFunc("GET /api/thumb", a.thumb)
	mux.HandleFunc("GET /api/photo", func(w http.ResponseWriter, r *http.Request) {
		f := r.FormValue("f")
		if _, ok := a.lib.Photo(f); !ok { // only indexed files: no path traversal
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, filepath.FromSlash(f))
	})
	mux.HandleFunc("GET /api/photo/meta", func(w http.ResponseWriter, r *http.Request) {
		f := r.FormValue("f")
		if _, ok := a.lib.Photo(f); !ok {
			fail(w, 404, errors.New("photo not found"))
			return
		}
		out, err := exiftool("-j", "-G1", "-q", "-q", filepath.FromSlash(f))
		var tags []map[string]any
		if json.Unmarshal(out, &tags) != nil || len(tags) == 0 {
			fail(w, 500, fmt.Errorf("couldn't read metadata: %v", err))
			return
		}
		writeJSON(w, tags[0])
	})
	mux.HandleFunc("POST /api/photo/rotate", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			File    string
			Degrees int
		}
		if err := readJSON(r, &req); err != nil {
			fail(w, 400, err)
			return
		}
		p, err := a.lib.Rotate(req.File, req.Degrees)
		if err != nil {
			fail(w, 400, err)
			return
		}
		writeJSON(w, p)
	})
	mux.HandleFunc("GET /filmimg/{pic}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "max-age=86400")
		http.ServeFileFS(w, r, os.DirFS(filepath.Join(a.db.Dir, "Images")), path.Base(r.PathValue("pic")))
	})
	return a.guard(local, mux)
}

func (a *App) guard(local bool, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		if local && !isLoopbackHost(r.Host) { // DNS rebinding
			http.Error(w, "bad host", 403)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if o := r.Header.Get("Origin"); o != "" {
				if u, err := url.Parse(o); err != nil || u.Host != r.Host {
					http.Error(w, "cross-site request refused", 403)
					return
				}
			}
		}
		public := !strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api/login" || r.URL.Path == "/api/state"
		if !local && !public && !a.authed(r) {
			fail(w, 401, errors.New("login required"))
			return
		}
		h.ServeHTTP(w, r)
	})
}

func isLoopbackHost(hostport string) bool {
	h, _, err := net.SplitHostPort(hostport)
	if err != nil {
		h = hostport
	}
	return h == "localhost" || net.ParseIP(h).IsLoopback()
}

// authed: remote requests need a session, unless no password is set and the listener is loopback-only.
func (a *App) authed(r *http.Request) bool {
	s := a.settings.Get()
	if s.WebUI.PasswordHash == "" {
		return isLoopbackHost(a.remoteAddr())
	}
	c, err := r.Cookie("emulsion")
	return err == nil && a.sessions.Valid(c.Value)
}

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	var req struct{ Password string }
	if err := readJSON(r, &req); err != nil {
		fail(w, 400, err)
		return
	}
	a.loginMu.Lock() // serialise attempts; failures cost a second
	defer a.loginMu.Unlock()
	if !checkPassword(a.settings.Get().WebUI, req.Password) {
		time.Sleep(time.Second)
		fail(w, 401, errors.New("wrong password"))
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "emulsion", Value: a.sessions.New(), Path: "/", MaxAge: int(sessionTTL.Seconds()),
		HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil})
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *App) state(w http.ResponseWriter, r *http.Request, local bool) {
	authed := local || a.authed(r)
	out := map[string]any{"authed": authed, "desktop": local && a.Desktop}
	if !authed {
		writeJSON(w, out)
		return
	}
	s := a.settings.Get()
	dbStatus, updated := a.db.Status()
	_, gitErr := exec.LookPath("git")
	_, exifErr := exec.LookPath("exiftool")
	a.remoteMu.Lock()
	remoteRunning, remoteErr := a.remote != nil, a.remoteErr
	a.remoteMu.Unlock()
	out["local"] = local
	out["library"] = map[string]any{"status": a.lib.Status(), "job": a.lib.Job()}
	out["filmdb"] = map[string]any{"status": dbStatus, "updated": updated, "edits": a.db.LocalEdits()}
	out["deps"] = map[string]bool{"git": gitErr == nil, "exiftool": exifErr == nil}
	out["settings"] = map[string]any{
		"libraries": s.Libraries, "importTo": s.ImportTo, "updateHours": s.UpdateHours,
		"autoUpdateCheck": !s.NoUpdateCheck,
		"structure":       s.Structure, "rename": s.Rename, "exportDirs": s.ExportDirs, "hotFolder": s.HotFolder, "editors": s.Editors, "apps": s.Apps,
		"webui": map[string]any{"enabled": s.WebUI.Enabled, "address": s.WebUI.Address, "hasPassword": s.WebUI.PasswordHash != "",
			"running": remoteRunning, "error": remoteErr, "urls": lanURLs(s.WebUI.Address)},
	}
	out["os"] = runtime.GOOS
	out["dataDir"] = filepath.ToSlash(a.DataDir)
	out["version"] = version
	up := a.updater.Info()
	out["update"] = map[string]any{"available": up.Available, "latest": up.Latest, "installing": up.Installing}
	out["updatedFrom"] = os.Getenv("EMULSION_UPDATED_FROM")
	writeJSON(w, out)
}

func lanURLs(addr string) []string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil
	}
	if host != "" && host != "0.0.0.0" && host != "::" {
		return []string{"http://" + addr}
	}
	var urls []string
	ifaces, _ := net.InterfaceAddrs()
	for _, ia := range ifaces {
		if ipn, ok := ia.(*net.IPNet); ok && ipn.IP.To4() != nil && !ipn.IP.IsLoopback() && !ipn.IP.IsLinkLocalUnicast() {
			urls = append(urls, "http://"+net.JoinHostPort(ipn.IP.String(), port))
		}
	}
	return urls
}

func (a *App) roll(w http.ResponseWriter, r *http.Request) {
	ro := a.lib.Roll(r.FormValue("dir"))
	if ro == nil {
		fail(w, 404, errors.New("roll not found"))
		return
	}
	writeJSON(w, ro)
}

func (a *App) rollSave(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Dir string
		Meta
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, 400, err)
		return
	}
	if err := a.lib.WriteRoll(req.Dir, req.Meta); err != nil {
		fail(w, 400, err)
		return
	}
	writeJSON(w, a.lib.Roll(req.Dir))
}

// importThumb previews a file in an import source before it's in the library.
func (a *App) importThumb(w http.ResponseWriter, r *http.Request) {
	root := filepath.Clean(filepath.FromSlash(r.FormValue("root")))
	p := filepath.Join(root, filepath.FromSlash(r.FormValue("rel")))
	if rel, err := filepath.Rel(root, p); err != nil || strings.HasPrefix(rel, "..") || !filepath.IsAbs(root) || !isImage(p) {
		http.NotFound(w, r)
		return
	}
	t, err := a.thumbs.Get(p, 480, 1)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeFile(w, r, t)
}

var batchRe = regexp.MustCompile(`^[a-z0-9]{1,32}$`)

// upload streams one file from the browser into staging; remote users import lab zips this way.
func (a *App) upload(w http.ResponseWriter, r *http.Request) {
	batch, name := r.FormValue("batch"), filepath.Base(filepath.FromSlash(r.FormValue("name")))
	if !batchRe.MatchString(batch) || skipName(name) || !(isImage(name) || ext(name) == "zip" || ext(name) == "xmp") {
		fail(w, 400, errors.New("only photos, sidecars and .zip files can be uploaded"))
		return
	}
	dir := filepath.Join(a.stagingDir(), "upload-"+batch)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fail(w, 500, err)
		return
	}
	to := uniquePath(filepath.Join(dir, name))
	f, err := os.Create(to + ".part")
	if err != nil {
		fail(w, 500, err)
		return
	}
	_, err = io.Copy(f, r.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(to+".part", to)
	}
	if err != nil {
		os.Remove(to + ".part")
		fail(w, 500, err)
		return
	}
	writeJSON(w, map[string]string{"Dir": filepath.ToSlash(dir), "Path": filepath.ToSlash(to)})
}

type filmItem struct {
	Line                                             int
	Name, Maker, Pic, Begin, End, Country, ISO, Info string
	Rolls                                            int
	Avail                                            int // 2 on the market, 1 not sure, 0 discontinued (the database's "Ava" column)
	Popular                                          int // >0 for an everyday stock that's still made; higher is more common
}

func toItem(line int, f []string, used map[string]int) filmItem {
	it := filmItem{Line: line, Name: strings.TrimSpace(f[colName]), Maker: f[colMaker], Pic: f[colPic], Begin: f[colBegin], End: f[colEnd], Country: f[colCountry],
		ISO: guessISO(f[colName]), Info: f[colInfo], Rolls: used[strings.ToLower(strings.TrimSpace(f[colName]))]}
	it.Avail, _ = strconv.Atoi(strings.TrimSpace(f[colAva]))
	if it.Avail == 2 {
		it.Popular = popularity(strings.ToLower(it.Name))
	}
	return it
}

// rank orders availability: popular in-production stocks, then anything on the market, then unsure, then discontinued.
func (it filmItem) rank() int {
	if it.Popular > 0 {
		return 3
	}
	return min(it.Avail, 2)
}

func (a *App) filmUsage() map[string]int {
	used := map[string]int{}
	for _, r := range a.lib.Rolls(false) {
		for _, f := range r.Films {
			used[strings.ToLower(f)]++
		}
	}
	return used
}

func (a *App) films(w http.ResponseWriter, r *http.Request) {
	lines, _, err := a.db.Lines()
	if err != nil {
		fail(w, 503, err)
		return
	}
	words := strings.Fields(strings.ToLower(r.FormValue("q")))
	shot := r.FormValue("shot") == "1"
	withPic := r.FormValue("pic") == "1"
	current := r.FormValue("current") == "1"
	limit, _ := strconv.Atoi(r.FormValue("limit"))
	offset, _ := strconv.Atoi(r.FormValue("offset"))
	if limit <= 0 || limit > 500 {
		limit = 60
	}
	used := a.filmUsage()
	items := []filmItem{}
	for i := 1; i < len(lines); i++ {
		l := strings.ToLower(lines[i])
		if slices.ContainsFunc(words, func(w string) bool { return !strings.Contains(l, w) }) {
			continue
		}
		f := strings.Split(lines[i], ";")
		it := toItem(i, f, used)
		if (shot && it.Rolls == 0) || (withPic && it.Pic == "") || (current && it.Avail != 2) {
			continue
		}
		items = append(items, it)
	}
	scores, inName := make([]int, len(items)), make([]bool, len(items))
	for i, it := range items {
		scores[i] = nameScore(it.Name, words)
		n := strings.ToLower(it.Name)
		inName[i] = !slices.ContainsFunc(words, func(w string) bool { return !strings.Contains(n, w) })
	}
	idx := make([]int, len(items))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(x, y int) bool {
		i, j := idx[x], idx[y]
		if shot && items[i].Rolls != items[j].Rolls {
			return items[i].Rolls > items[j].Rolls
		}
		// Films whose name matches beat notes-only matches; then films you can still buy beat historical variants.
		if inName[i] != inName[j] {
			return inName[i]
		}
		if items[i].rank() != items[j].rank() {
			return items[i].rank() > items[j].rank()
		}
		if items[i].Popular != items[j].Popular {
			return items[i].Popular > items[j].Popular
		}
		return scores[i] > scores[j]
	})
	sorted := make([]filmItem, len(items))
	for x, i := range idx {
		sorted[x] = items[i]
	}
	items = sorted
	total := len(items)
	items = items[min(offset, total):min(offset+limit, total)]
	writeJSON(w, map[string]any{"total": total, "items": items})
}

func (a *App) film(w http.ResponseWriter, r *http.Request) {
	lines, _, err := a.db.Lines()
	if err != nil {
		fail(w, 503, err)
		return
	}
	line, _ := strconv.Atoi(r.FormValue("line"))
	if name := strings.ToLower(r.FormValue("name")); name != "" {
		line = -1
		for i := 1; i < len(lines); i++ {
			if strings.ToLower(strings.Split(lines[i], ";")[colName]) == name {
				line = i
				break
			}
		}
	}
	if line < 0 || line >= len(lines) {
		fail(w, 404, errors.New("film not in database"))
		return
	}
	header := strings.Split(lines[0], ";")
	fields, orig := make([]string, len(header)), ""
	if line > 0 {
		orig = lines[line]
		fields = strings.Split(orig, ";")
	}
	rolls := []*Roll{}
	for _, ro := range a.lib.Rolls(false) {
		if line > 0 && slices.ContainsFunc(ro.Films, func(f string) bool { return strings.EqualFold(f, fields[colName]) }) {
			rolls = append(rolls, ro)
		}
	}
	avail := -1
	if line > 0 {
		avail = toItem(line, fields, nil).Avail
	}
	writeJSON(w, map[string]any{"line": line, "orig": orig, "header": header, "fields": fields, "iso": guessISO(fields[colName]), "rolls": rolls, "avail": avail})
}

func (a *App) filmSave(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Line   int
		Orig   string
		Fields []string
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, 400, err)
		return
	}
	line, err := a.db.Save(req.Line, req.Orig, req.Fields)
	if err != nil {
		fail(w, 400, err)
		return
	}
	writeJSON(w, map[string]int{"line": line})
}

// browse lists sub-folders so both desktop and remote users can pick paths on the machine running Emulsion.
func (a *App) browse(w http.ResponseWriter, r *http.Request) {
	p := r.FormValue("path")
	type entry struct{ Name, Path string }
	out := struct {
		Path, Parent string
		Dirs         []entry
		Zips         []entry
		Images       int
		Places       []entry
	}{Dirs: []entry{}, Zips: []entry{}}
	home, _ := os.UserHomeDir()
	if home != "" {
		out.Places = append(out.Places, entry{"Home", filepath.ToSlash(home)})
		if _, err := os.Stat(filepath.Join(home, "Pictures")); err == nil {
			out.Places = append(out.Places, entry{"Pictures", filepath.ToSlash(filepath.Join(home, "Pictures"))})
		}
	}
	if p == "" {
		if runtime.GOOS == "windows" {
			for c := 'A'; c <= 'Z'; c++ {
				if _, err := os.Stat(string(c) + `:\`); err == nil {
					out.Dirs = append(out.Dirs, entry{string(c) + ":", string(c) + ":/"})
				}
			}
			writeJSON(w, out)
			return
		}
		p = "/"
	}
	p = filepath.Clean(filepath.FromSlash(p))
	entries, err := os.ReadDir(p)
	if err != nil {
		fail(w, 400, err)
		return
	}
	out.Path = filepath.ToSlash(p)
	if parent := filepath.Dir(p); parent != p {
		out.Parent = filepath.ToSlash(parent)
	} else if runtime.GOOS == "windows" {
		out.Parent = "" // drive list
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") || strings.HasPrefix(e.Name(), "$") {
			continue
		}
		if e.IsDir() {
			out.Dirs = append(out.Dirs, entry{e.Name(), filepath.ToSlash(filepath.Join(p, e.Name()))})
		} else if isImage(e.Name()) {
			out.Images++
		} else if ext(e.Name()) == "zip" {
			out.Zips = append(out.Zips, entry{e.Name(), filepath.ToSlash(filepath.Join(p, e.Name()))})
		}
	}
	sort.Slice(out.Dirs, func(i, j int) bool { return naturalLess(out.Dirs[i].Name, out.Dirs[j].Name) })
	writeJSON(w, out)
}

func (a *App) saveSettings(w http.ResponseWriter, r *http.Request, local bool) {
	var req struct {
		Libraries   []string
		ImportTo    string
		UpdateHours int
		WebUI       *struct {
			Enabled  bool
			Address  string
			Password string
		}
		AutoUpdateCheck   *bool
		Structure, Rename *string
		ExportDirs        *[]string
		HotFolder         *HotFolder
		Apps              *map[string]string
		Editors           *[]Editor
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, 400, err)
		return
	}
	libs := []string{}
	for _, l := range req.Libraries {
		st, err := os.Stat(filepath.FromSlash(l))
		if err != nil || !st.IsDir() {
			fail(w, 400, errors.New("not a folder: "+l))
			return
		}
		abs, _ := filepath.Abs(filepath.FromSlash(l))
		libs = addUniq(libs, filepath.ToSlash(abs))
	}
	importTo := req.ImportTo
	if !slices.Contains(libs, importTo) && len(libs) > 0 {
		importTo = libs[0]
	}
	if req.WebUI != nil {
		if !local {
			fail(w, 403, errors.New("remote access settings can only be changed from the desktop app"))
			return
		}
		if _, _, err := net.SplitHostPort(req.WebUI.Address); err != nil {
			fail(w, 400, errors.New("address must look like 0.0.0.0:8080"))
			return
		}
		if req.WebUI.Enabled && req.WebUI.Password == "" && a.settings.Get().WebUI.PasswordHash == "" && !isLoopbackHost(req.WebUI.Address) {
			fail(w, 400, errors.New("set a password before enabling remote access"))
			return
		}
		if req.WebUI.Password != "" && len(req.WebUI.Password) < 8 {
			fail(w, 400, errors.New("password must be at least 8 characters"))
			return
		}
	}
	if (req.Apps != nil || req.Editors != nil) && !local {
		fail(w, 403, errors.New("app locations can only be changed from the desktop app"))
		return
	}
	if req.Structure != nil {
		if _, err := folderFor(*req.Structure, tokens(ImportRequest{}, "Roll", "", "2026-01-31")); err != nil {
			fail(w, 400, err)
			return
		}
	}
	if req.HotFolder != nil && req.HotFolder.Enabled {
		if st, err := os.Stat(filepath.FromSlash(req.HotFolder.Path)); err != nil || !st.IsDir() {
			fail(w, 400, errors.New("hot folder must be an existing folder"))
			return
		}
		if inAnyLibrary(filepath.FromSlash(req.HotFolder.Path), libs) {
			fail(w, 400, errors.New("the hot folder can't be inside a library"))
			return
		}
	}
	oldLibs := a.settings.Get().Libraries
	err := a.settings.Update(func(s *Settings) {
		s.Libraries, s.ImportTo, s.UpdateHours = libs, importTo, max(1, req.UpdateHours)
		if req.AutoUpdateCheck != nil {
			s.NoUpdateCheck = !*req.AutoUpdateCheck
		}
		if req.Structure != nil {
			s.Structure = *req.Structure
		}
		if req.Rename != nil {
			s.Rename = *req.Rename
		}
		if req.ExportDirs != nil {
			s.ExportDirs = slices.DeleteFunc(*req.ExportDirs, func(d string) bool { return strings.TrimSpace(d) == "" })
		}
		if req.HotFolder != nil {
			s.HotFolder = *req.HotFolder
		}
		if req.Apps != nil {
			s.Apps = *req.Apps
		}
		if req.Editors != nil {
			s.Editors = *req.Editors
		}
		if req.WebUI != nil {
			s.WebUI.Enabled, s.WebUI.Address = req.WebUI.Enabled, req.WebUI.Address
			if req.WebUI.Password != "" {
				s.WebUI.PasswordHash, s.WebUI.Salt = hashPassword(req.WebUI.Password)
				a.sessions.Clear()
			}
		}
	})
	if err != nil {
		fail(w, 500, err)
		return
	}
	if !slices.Equal(oldLibs, libs) || req.ExportDirs != nil {
		go a.lib.Scan(libs)
	}
	if req.WebUI != nil && a.Desktop {
		a.ApplyRemote()
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *App) thumb(w http.ResponseWriter, r *http.Request) {
	f := r.FormValue("f")
	photo, ok := a.lib.Photo(f)
	if !ok {
		http.NotFound(w, r)
		return
	}
	size := 480
	switch r.FormValue("s") {
	case "l":
		size = 2000
	case "f":
		size = 8000 // zoomed-in viewing of formats browsers can't show, like TIFF and RAW
	}
	p, err := a.thumbs.Get(filepath.FromSlash(f), size, photo.Orientation)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	// Revalidate rather than cache for days: rotating a photo changes its thumbnail under the same URL.
	w.Header().Set("Cache-Control", "private, no-cache")
	http.ServeFile(w, r, p)
}

// ---- remote web UI listener (desktop mode) ----

func (a *App) remoteAddr() string {
	a.remoteMu.Lock()
	defer a.remoteMu.Unlock()
	if a.remote != nil {
		return a.remote.Addr
	}
	return a.settings.Get().WebUI.Address
}

// ApplyRemote starts, restarts or stops the remote web UI to match settings.
func (a *App) ApplyRemote() {
	s := a.settings.Get().WebUI
	a.remoteMu.Lock()
	defer a.remoteMu.Unlock()
	if a.remote != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		a.remote.Shutdown(ctx)
		cancel()
		a.remote = nil
	}
	a.remoteErr = ""
	if !s.Enabled {
		return
	}
	// After an update restart the old process may still hold the port for a moment.
	ln, err := net.Listen("tcp", s.Address)
	for i := 0; err != nil && i < 20 && os.Getenv("EMULSION_UPDATED_FROM") != ""; i++ {
		time.Sleep(500 * time.Millisecond)
		ln, err = net.Listen("tcp", s.Address)
	}
	if err != nil {
		a.remoteErr = err.Error()
		return
	}
	srv := &http.Server{Addr: s.Address, Handler: a.Handler(false), ReadHeaderTimeout: 10 * time.Second}
	a.remote = srv
	go srv.Serve(ln)
	logf("remote web UI on %s", s.Address)
}
