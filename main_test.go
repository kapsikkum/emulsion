package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"image"
	"image/jpeg"
	"io/fs"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestEditLine(t *testing.T) {
	lines := func() []string { return []string{"a;b;Name", "1;2;Portra"} }
	if _, err := editLine(lines(), 1, "stale", []string{"1", "2", "X"}); err == nil {
		t.Fatal("stale orig should conflict")
	}
	if _, err := editLine(lines(), 1, "1;2;Portra", []string{"1", "2;", "X"}); err == nil {
		t.Fatal("semicolon should be rejected")
	}
	if _, err := editLine(lines(), 0, "", []string{"", "", " "}); err == nil {
		t.Fatal("blank name should be rejected")
	}
	got, err := editLine(lines(), 1, "1;2;Portra", []string{"1", "2", "Portra 400 "})
	if err != nil || got[1] != "1;2;Portra 400" {
		t.Fatal(got, err)
	}
	got, err = editLine(lines(), 0, "", []string{"", "", "New"})
	if err != nil || len(got) != 3 || got[2] != ";;New" {
		t.Fatal(got, err)
	}
}

func TestPhotoJSON(t *testing.T) {
	var ps []Photo
	err := json.Unmarshal([]byte(`[{"SourceFile":"/r/a.jpg","Model":600,"ISO":400,"Subject":["x","film:Kodak Gold 200"]},{"SourceFile":"/r/b.jpg","Subject":"film:HP5"}]`), &ps)
	if err != nil || ps[0].Model != "600" || ps[0].ISO != "400" || ps[0].Film() != "Kodak Gold 200" || ps[1].Film() != "HP5" || ps[1].Camera() != "" {
		t.Fatal(ps, err)
	}
}

func TestMetaArgs(t *testing.T) {
	args, err := metaArgs(Meta{Film: "HP5", Date: "2026-05-03", Make: "Nikon"}, []string{"Gold"})
	want := []string{"-Make=Nikon", "-DateTimeOriginal=2026:05:03 12:00:00", "-XMP-dc:Subject-=film:Gold", "-XMP-dc:Subject+=film:HP5"}
	for _, w := range want {
		if !slices.Contains(args, w) {
			t.Fatal(w, args, err)
		}
	}
	if _, err := metaArgs(Meta{Date: "03/05/2026"}, nil); err == nil {
		t.Fatal("bad date accepted")
	}
	if _, err := metaArgs(Meta{Film: "a\n-o=/etc"}, nil); err == nil {
		t.Fatal("newline injection accepted")
	}
}

func TestGuessISO(t *testing.T) {
	for name, want := range map[string]string{"Kodak Portra 400": "400", "3M ColorPrint ISO 100": "100", "Ilford HP5 Plus": "", "Cinestill 800T": "800", "Fuji Superia X-TRA 400 #xtra": "400"} {
		if got := guessISO(name); got != want {
			t.Errorf("%q: got %q want %q", name, got, want)
		}
	}
}

func TestNaturalLess(t *testing.T) {
	s := []string{"f10.jpg", "f2.jpg", "f1.jpg", "F02b.jpg"}
	sort.Slice(s, func(i, j int) bool { return naturalLess(s[i], s[j]) })
	if !slices.Equal(s, []string{"f1.jpg", "f2.jpg", "F02b.jpg", "f10.jpg"}) {
		t.Fatal(s)
	}
}

func TestFolderFor(t *testing.T) {
	vals := tokens(ImportRequest{Meta: Meta{Film: "HP5 (black/white)", Date: "2026-05-03"}}, "Lisbon: day 1", "", "")
	got, err := folderFor("{yyyy}/{date} {film}/{name}", vals)
	if want := filepath.Join("2026", "2026-05-03 HP5 (black-white)", "Lisbon- day 1"); err != nil || got != want {
		t.Fatalf("got %q want %q (%v)", got, want, err)
	}
	if got, _ := folderFor("../../{name}", vals); strings.Contains(got, "..") {
		t.Fatalf("template escaped the library: %q", got)
	}
	if _, err := folderFor("{film}", tokens(ImportRequest{}, "", "", "")); err == nil {
		t.Fatal("empty folder accepted")
	}
	if got := fileName("{name}_{seq}", vals, "IMG_1.NEF", 4, 36); got != "Lisbon- day 1_05.nef" {
		t.Fatal(got)
	}
}

func TestExtractZipSlip(t *testing.T) {
	dir := t.TempDir()
	zp := filepath.Join(dir, "lab.zip")
	f, _ := os.Create(zp)
	zw := zip.NewWriter(f)
	for _, name := range []string{"../evil.jpg", `..\evil2.jpg`, "Roll 12/scan01.jpg", "__MACOSX/._scan01.jpg", "readme.txt"} {
		w, _ := zw.Create(name)
		w.Write([]byte("x"))
	}
	zw.Close()
	f.Close()
	dest := filepath.Join(dir, "out")
	if err := extractZip(zp, dest); err != nil {
		t.Fatal(err)
	}
	var got []string
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, _ error) error {
		if !d.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			got = append(got, filepath.ToSlash(rel))
		}
		return nil
	})
	slices.Sort(got)
	want := []string{"lab.zip", "out/.complete", "out/Roll 12/scan01.jpg", "out/evil.jpg", "out/evil2.jpg"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestMergeMeta(t *testing.T) {
	var raws []rawMeta
	json.Unmarshal([]byte(`[
		{"SourceFile":"/r/IMG_1.NEF","Make":"Nikon","Model":"Z6"},
		{"SourceFile":"/r/IMG_1.xmp","Make":"Leica","Model":"M6","Subject":["film:Portra 400"]},
		{"SourceFile":"/r/export/IMG_1.jpg","Make":"Nikon","Model":"Z6","CaptureCameraMake":"Canon","CaptureCameraModel":"AE-1","CaptureFilmStock":"HP5"}
	]`), &raws)
	ps := mergeMeta(raws)
	if len(ps) != 2 || ps[0].Camera() != "Leica M6" || ps[0].Film() != "Portra 400" || ps[1].Camera() != "Canon AE-1" || ps[1].Film() != "HP5" {
		t.Fatalf("%+v", ps)
	}
}

func TestImportEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("exiftool"); err != nil {
		t.Skip("exiftool not installed")
	}
	root := t.TempDir()
	lib, src := filepath.Join(root, "lib"), filepath.Join(root, "card", "DCIM")
	os.MkdirAll(lib, 0o755)
	os.MkdirAll(filepath.Join(src, "sub"), 0o755)
	os.WriteFile(filepath.Join(src, "IMG_1.NEF"), []byte("raw"), 0o644)
	os.WriteFile(filepath.Join(src, "IMG_1.xmp"), []byte(`<x:xmpmeta xmlns:x="adobe:ns:meta/"/>`), 0o644)
	os.WriteFile(filepath.Join(src, "sub", "IMG_2.NEF"), []byte("raw2"), 0o644)

	a := NewApp(filepath.Join(root, "data"), false)
	a.settings.Update(func(s *Settings) { s.Libraries = []string{filepath.ToSlash(lib)} })
	s, err := a.OpenSource(filepath.ToSlash(src), true, false)
	if err != nil || len(s.Files) != 2 || s.Files[1].Group != "sub" {
		t.Fatalf("%+v %v", s, err)
	}
	req := ImportRequest{Source: s.Source, Subfolders: true, Mode: "copy", Dest: filepath.ToSlash(lib), Structure: "{yyyy}/{name}", Rename: "{name}-{seq}", Name: "Roll 7", Meta: Meta{Date: "2025-02-01"}}
	plans, err := a.PlanImport(req, s)
	if err != nil || len(plans) != 1 || plans[0].Dir != filepath.Join(lib, "2025", "Roll 7") {
		t.Fatalf("%+v %v", plans, err)
	}
	if err := a.runImport(req, s, plans); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"Roll 7-01.nef", "Roll 7-01.xmp", "Roll 7-02.nef"} {
		if _, err := os.Stat(filepath.Join(lib, "2025", "Roll 7", f)); err != nil {
			t.Error(err)
		}
	}
	if _, err := os.Stat(filepath.Join(src, "IMG_1.NEF")); err != nil {
		t.Error("copy removed the source")
	}
	// Tag on import (changes file sizes), then re-scanning the source still flags duplicates.
	os.WriteFile(filepath.Join(src, "photo.jpg"), jpegBytes(t), 0o644)
	s, _ = a.OpenSource(filepath.ToSlash(src), true, false)
	req.Files, req.Film, req.Rename = []string{"photo.jpg"}, "HP5", ""
	plans, _ = a.PlanImport(req, s)
	if err := a.runImport(req, s, plans); err != nil {
		t.Fatal(err)
	}
	// Re-scanning the source now flags duplicates.
	if s2, _ := a.OpenSource(filepath.ToSlash(src), true, false); slices.ContainsFunc(s2.Files, func(f ImportFile) bool { return !f.Dup }) {
		t.Errorf("duplicate not detected: %+v", s2.Files)
	}
}

func jpegBytes(t *testing.T) []byte {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 8, 8)), nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestNameScore(t *testing.T) {
	w := strings.Fields("portra 160")
	if nameScore("Kodak Portra 160 (new)", w) <= nameScore("AGFA AGFACOLOR Portrait 160 Professional", w) {
		t.Fatal("exact words should outrank partial matches")
	}
}

func TestHotFolder(t *testing.T) {
	if _, err := exec.LookPath("exiftool"); err != nil {
		t.Skip("exiftool not installed")
	}
	root := t.TempDir()
	lib, hot := filepath.Join(root, "lib"), filepath.Join(root, "hot")
	os.MkdirAll(lib, 0o755)
	os.MkdirAll(hot, 0o755)
	zp := filepath.Join(hot, "Lab order 77.zip")
	f, _ := os.Create(zp)
	zw := zip.NewWriter(f)
	for _, n := range []string{"77/a.jpg", "77/b.jpg"} {
		w, _ := zw.Create(n)
		w.Write(jpegBytes(t))
	}
	zw.Close()
	f.Close()

	a := NewApp(filepath.Join(root, "data"), false)
	a.settings.Update(func(s *Settings) {
		s.Libraries, s.ImportTo, s.Structure = []string{filepath.ToSlash(lib)}, filepath.ToSlash(lib), "{source}"
		s.HotFolder = HotFolder{Enabled: true, Path: filepath.ToSlash(hot)}
	})
	a.checkHotFolder(map[string]bool{}) // too fresh: still "downloading"
	if _, err := os.Stat(zp); err != nil {
		t.Fatal("imported a zip that was still being written")
	}
	old := time.Now().Add(-10 * time.Minute)
	os.Chtimes(zp, old, old)
	a.checkHotFolder(map[string]bool{})
	for _, p := range []string{filepath.Join(lib, "Lab order 77", "a.jpg"), filepath.Join(hot, "Imported", "Lab order 77.zip")} {
		if _, err := os.Stat(p); err != nil {
			t.Error(err)
		}
	}
}

// A fresh install has no settings file; the UI must still get arrays, not null (it crashed on first launch).
func TestFreshStateHasEmptyLists(t *testing.T) {
	a := NewApp(t.TempDir(), true)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/state", nil)
	req.Host = "127.0.0.1:1234"
	a.Handler(true).ServeHTTP(rec, req)
	var out struct {
		Settings map[string]any
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err, rec.Body.String())
	}
	for _, k := range []string{"libraries", "exportDirs", "editors", "apps"} {
		if out.Settings[k] == nil {
			t.Errorf("%s is null in %s", k, rec.Body.String())
		}
	}
}

func TestTidyName(t *testing.T) {
	for in, want := range map[string]string{
		"Roll 1 - Portra 400":  "Portra 400",
		"roll_03_-_Portra_400": "Portra 400",
		"#12 Lisbon":           "Lisbon",
		"03. Beach day":        "Beach day",
		"2024-05 Lisbon":       "2024-05 Lisbon", // dates are kept
		"00012345":             "00012345",       // nothing left after tidying: keep the original
		"Order 48213":          "Order 48213",
	} {
		if got := tidyName(in); got != want {
			t.Errorf("tidyName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGuessFilm(t *testing.T) {
	lines := []string{"h;h;Name", ";;Kodak Portra 400 (new)", ";;Kodak Professional Portra 400 VC", ";;Ilford HP5 PLUS 400 HP5+ (black box)", ";;Ilford HP5", ";;Fujicolor Daylight 100"}
	for folder, want := range map[string]string{
		"Roll 1 - Portra 400": "Kodak Portra 400 (new)",
		"Roll 2 - HP5":        "Ilford HP5",
		"Beach day":           "",
		"Roll 3":              "",
	} {
		if got := guessFilm(lines, folder); got != want {
			t.Errorf("guessFilm(%q) = %q, want %q", folder, got, want)
		}
	}
}

func TestPlanPerRollOverrides(t *testing.T) {
	root := t.TempDir()
	lib := filepath.Join(root, "lib")
	os.MkdirAll(lib, 0o755)
	a := NewApp(filepath.Join(root, "data"), false)
	a.settings.Update(func(s *Settings) { s.Libraries = []string{filepath.ToSlash(lib)} })
	src := &ImportSource{Name: "Order 1", Files: []ImportFile{
		{Rel: "Roll 1 - Portra 400/1.jpg", Group: "Roll 1 - Portra 400", Date: "2026-05-03"},
		{Rel: "Roll 2 - HP5/1.jpg", Group: "Roll 2 - HP5", Date: "2026-05-04"},
	}}
	req := ImportRequest{Mode: "copy", Dest: filepath.ToSlash(lib), Structure: "{yyyy}/{film}/{name}", Split: true, Meta: Meta{Make: "Nikon", Film: "Default film"},
		Rolls: []RollOverride{{Group: "Roll 1 - Portra 400", Name: "Lisbon", Meta: Meta{Film: "Kodak Portra 400", Date: "2025-12-31"}}}}
	plans, err := a.PlanImport(req, src)
	if err != nil {
		t.Fatal(err)
	}
	if plans[0].Dir != filepath.Join(lib, "2025", "Kodak Portra 400", "Lisbon") || plans[0].Meta.Make != "Nikon" {
		t.Errorf("roll 1: %+v", plans[0])
	}
	if plans[1].Dir != filepath.Join(lib, "2026", "Default film", "HP5") || plans[1].Meta.Film != "Default film" {
		t.Errorf("roll 2 should get the tidied name and the default film: %+v", plans[1])
	}
	req.Rolls = []RollOverride{{Group: "Roll 1 - Portra 400", Name: "Same"}, {Group: "Roll 2 - HP5", Name: "same"}}
	req.Structure = "{name}"
	if _, err := a.PlanImport(req, src); err == nil {
		t.Error("two rolls in one folder should be refused")
	}
}
