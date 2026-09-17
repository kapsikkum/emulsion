package main

// Film database: a git clone of the Open Source Film Database.
// User edits are local commits; updates are `git pull` (user edits win on conflicts).

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
)

const upstream = "https://github.com/dxdatabase/Open-source-film-database"

// CSV columns (upstream order).
const (
	colDX, colDXFull, colName, colInfo, colMaker, colReliability, colCountry, colBegin, colEnd, colDistributor, colAva, colPic = 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11
)

type FilmDB struct {
	Dir     string
	mu      sync.Mutex // serialises git operations and CSV writes
	status  string
	updated time.Time
}

func (db *FilmDB) csv() string { return filepath.Join(db.Dir, "film_database.csv") }

func (db *FilmDB) git(dir string, args ...string) (string, error) {
	args = append([]string{"-C", dir, "-c", "user.name=Emulsion", "-c", "user.email=emulsion@localhost", "-c", "core.autocrlf=false"}, args...)
	cmd := exec.Command("git", args...)
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func (db *FilmDB) setStatus(s string) {
	db.status = s
	logf("film db: %s", s)
}

func (db *FilmDB) Status() (string, time.Time) {
	return db.status, db.updated
}

// Sync clones on first run, otherwise pulls upstream and merges over local edits.
func (db *FilmDB) Sync() {
	db.mu.Lock()
	defer db.mu.Unlock()
	if _, err := os.Stat(db.csv()); err != nil {
		db.setStatus("Downloading film database…")
		tmp := db.Dir + ".tmp"
		os.RemoveAll(tmp)
		os.MkdirAll(filepath.Dir(db.Dir), 0o755)
		if out, err := db.git(filepath.Dir(db.Dir), "clone", "--config", "core.autocrlf=false", upstream, filepath.Base(tmp)); err != nil {
			db.setStatus("Download failed: " + firstLine(out, err))
			return
		}
		os.RemoveAll(db.Dir)
		if err := os.Rename(tmp, db.Dir); err != nil {
			db.setStatus("Download failed: " + err.Error())
			return
		}
	} else {
		db.setStatus("Checking for updates…")
		db.git(db.Dir, "config", "core.autocrlf", "false")
		if out, err := db.git(db.Dir, "pull", "--no-rebase", "--no-edit", "-X", "ours"); err != nil {
			db.git(db.Dir, "merge", "--abort")
			db.setStatus("Update failed: " + firstLine(out, err))
			return
		}
	}
	db.updated = time.Now()
	db.setStatus("Up to date")
}

// firstLine picks the most useful line of git output (errors over hints).
func firstLine(out string, err error) string {
	if out == "" {
		return err.Error()
	}
	lines := strings.Split(out, "\n")
	for _, l := range lines {
		if strings.HasPrefix(l, "error") || strings.HasPrefix(l, "fatal") || strings.HasPrefix(l, "CONFLICT") {
			return l
		}
	}
	return lines[len(lines)-1]
}

func (db *FilmDB) Lines() ([]string, string, error) {
	b, err := os.ReadFile(db.csv())
	if err != nil {
		return nil, "", errors.New("film database not downloaded yet")
	}
	s := string(b)
	eol := "\n"
	if strings.Contains(s, "\r\n") {
		eol = "\r\n"
	}
	return strings.Split(strings.TrimSuffix(s, eol), eol), eol, nil
}

// editLine replaces line (or appends when line == 0) if it still equals orig.
func editLine(lines []string, line int, orig string, fields []string) ([]string, error) {
	if len(lines) == 0 || len(fields) != strings.Count(lines[0], ";")+1 {
		return nil, errors.New("wrong number of fields")
	}
	for i, f := range fields {
		fields[i] = strings.TrimSpace(f)
		if strings.ContainsAny(f, ";\r\n") {
			return nil, errors.New("fields can't contain ; or line breaks")
		}
	}
	if fields[colName] == "" {
		return nil, errors.New("name is required")
	}
	nl := strings.Join(fields, ";")
	if line == 0 {
		return append(lines, nl), nil
	}
	if line < 0 || line >= len(lines) || lines[line] != orig {
		return nil, errors.New("this film changed since you opened it (database updated?) — reopen and retry")
	}
	lines[line] = nl
	return lines, nil
}

// Save writes one film row and commits it. Returns the line number.
func (db *FilmDB) Save(line int, orig string, fields []string) (int, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	lines, eol, err := db.Lines()
	if err != nil {
		return 0, err
	}
	// Match the line endings git stores, not the checkout: a CRLF rewrite would touch every line and break merges.
	if out, _ := db.git(db.Dir, "ls-files", "--eol", "film_database.csv"); strings.HasPrefix(out, "i/lf") {
		eol = "\n"
	} else if strings.HasPrefix(out, "i/crlf") {
		eol = "\r\n"
	}
	if line > 0 && line < len(lines) && strings.Join(fields, ";") == lines[line] {
		return line, nil
	}
	if lines, err = editLine(lines, line, orig, fields); err != nil {
		return 0, err
	}
	if err := os.WriteFile(db.csv(), []byte(strings.Join(lines, eol)+eol), 0o644); err != nil {
		return 0, err
	}
	msg := "Edit film: "
	if line == 0 {
		msg, line = "Add film: ", len(lines)-1
	}
	if out, err := db.git(db.Dir, "commit", "-m", msg+fields[colName], "--", "film_database.csv"); err != nil {
		db.git(db.Dir, "checkout", "--", "film_database.csv")
		return 0, errors.New("git commit failed: " + firstLine(out, err))
	}
	return line, nil
}

type LocalEdit struct {
	Hash, Message, Date string
}

// LocalEdits lists commits the user made that aren't upstream, hiding edits that were reverted.
func (db *FilmDB) LocalEdits() []LocalEdit {
	out, err := db.git(db.Dir, "log", "--no-merges", "--format=%H%x1f%h%x1f%s%x1f%ad%x1f%b%x1e", "--date=short", "@{upstream}..HEAD")
	edits := []LocalEdit{}
	if err != nil {
		return edits
	}
	reverted := map[string]bool{}
	var recs [][]string
	for _, rec := range strings.Split(out, "\x1e") {
		if p := strings.Split(strings.TrimSpace(rec), "\x1f"); len(p) == 5 {
			if m := revertRe.FindStringSubmatch(p[4]); m != nil {
				reverted[m[1]], reverted[p[0]] = true, true
			}
			recs = append(recs, p)
		}
	}
	for _, p := range recs {
		if !reverted[p[0]] {
			edits = append(edits, LocalEdit{p[1], p[2], p[3]})
		}
	}
	return edits
}

var revertRe = regexp.MustCompile(`This reverts commit ([0-9a-f]{40})`)

var hashRe = regexp.MustCompile(`^[0-9a-f]{4,40}$`)

// Revert undoes one local edit with a new commit.
func (db *FilmDB) Revert(hash string) error {
	if !hashRe.MatchString(hash) {
		return errors.New("bad commit id")
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	if out, err := db.git(db.Dir, "revert", "--no-edit", hash); err != nil {
		db.git(db.Dir, "revert", "--abort")
		return errors.New("revert failed: " + firstLine(out, err))
	}
	return nil
}

// popularFilms are everyday stocks, most common first. Only films the database marks as on the market count,
// so a 1990s "Kodak Gold 200" variant doesn't jump ahead of the one you can buy today.
var popularFilms = func() []*regexp.Regexp {
	var out []*regexp.Regexp
	for _, p := range []string{
		`kodak.*portra`, `kodak gold`, `kodak ultra ?max`, `kodak colou?r ?plus`, `kodak.*ektar`, `kodak.*pro ?image`,
		`fujicolor|fujifilm (200|400)|\bc200\b`, `ilford hp5`, `kodak.*tri-x`, `ilford fp4`, `kodak t-?max`, `ilford delta`,
		`cinestill`, `kodak.*ektachrome`, `velvia|provia`, `acros`, `ilford xp2`, `ilford pan ?f`, `ilford (sfx|ortho)`,
		`kentmere`, `harman phoenix`, `lomography colou?r|lomochrome|lomography .*(berlin|lady grey|earl grey)`,
		`fomapan|retropan`, `rollei (rpx|retro|superpan)`, `adox (chs|silvermax|scala)`, `agfa ?photo apx|agfa apx`, `kosmo foto`,
	} {
		out = append(out, regexp.MustCompile(p))
	}
	return out
}()

// popularity is 0 for ordinary films, otherwise higher for more common stocks.
func popularity(lowerName string) int {
	for i, re := range popularFilms {
		if re.MatchString(lowerName) {
			return len(popularFilms) - i
		}
	}
	return 0
}

var isoRe = regexp.MustCompile(`(?i)(?:iso|asa|ei)\s*(\d{2,5})|(\d{2,5})\s*(?:iso|asa|dx|t\b|d\b|n\b)|\b(25|32|50|64|80|100|125|160|200|250|320|400|500|640|800|1000|1600|3200|6400)\b`)

// guessISO pulls a speed out of a film name ("Kodak Portra 400" -> "400").
func guessISO(name string) string {
	m := isoRe.FindStringSubmatch(name)
	for _, g := range m[min(1, len(m)):] {
		if g != "" {
			return g
		}
	}
	return ""
}

// nameScore ranks a film name against search words: whole words beat word prefixes beat substrings,
// so "portra 160" finds Kodak Portra 160 before AGFA Portrait 160.
func nameScore(name string, words []string) int {
	fields := strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	score := 0
	for _, w := range words {
		best := 0
		for _, f := range fields {
			switch {
			case f == w:
				best = max(best, 3)
			case strings.HasPrefix(f, w):
				best = max(best, 2)
			case strings.Contains(f, w):
				best = max(best, 1)
			}
		}
		score += best
	}
	return score
}
