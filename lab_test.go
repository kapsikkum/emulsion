package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

// Names as they arrive from the lab (Fujifilm Frontier exports).
func frontierRoll(order string, roll, n int, countDown bool) []string {
	var out []string
	for i := 0; i < n; i++ {
		edge := fmt.Sprint(i + 1)
		if countDown {
			edge = fmt.Sprintf("%dA", n-1-i)
		}
		out = append(out, fmt.Sprintf("%s-R%d-%02d-%s.JPG", order, roll, i, edge))
	}
	return out
}

func TestParseLabName(t *testing.T) {
	f, ok := parseLabName("B001738-R1-00-36A.JPG")
	if !ok || f.Order != "B001738" || f.Roll != 1 || f.Seq != 0 || f.Edge != "36A" {
		t.Fatalf("%+v %v", f, ok)
	}
	if f, ok := parseLabName("b002650-r2-34-35.jpg"); !ok || f.key() != "B002650-R2" || f.Seq != 34 {
		t.Fatalf("%+v %v", f, ok)
	}
	for _, bad := range []string{"IMG_4521.JPG", "scan_1.jpg", "B001738-R1.JPG", "2024-05-03 Lisbon.jpg"} {
		if _, ok := parseLabName(bad); ok {
			t.Errorf("%q parsed as a lab name", bad)
		}
	}
}

func TestRollKeys(t *testing.T) {
	single := frontierRoll("B001738", 1, 34, true)
	if keys := rollKeys(single); keys != nil {
		t.Errorf("one roll should not split: %v", keys[:3])
	}

	mixed := slices.Concat(frontierRoll("B002650", 1, 35, false), frontierRoll("B002650", 2, 24, false), frontierRoll("B001738", 1, 12, true))
	keys := rollKeys(mixed)
	if distinct(keys) != 3 || keys[0] != "B002650-R1" || keys[40] != "B002650-R2" || keys[len(keys)-1] != "B001738-R1" {
		t.Errorf("frontier split wrong: %d groups, %v %v %v", distinct(keys), keys[0], keys[40], keys[len(keys)-1])
	}

	// Other scanners: prefixes that differ only in their numbers.
	var noritsu, digitsOnly []string
	for i := 1; i <= 5; i++ {
		noritsu = append(noritsu, fmt.Sprintf("R1-00123-%04d.JPG", i), fmt.Sprintf("R2-00123-%04d.JPG", i))
		digitsOnly = append(digitsOnly, fmt.Sprintf("00001234%04d.jpg", i), fmt.Sprintf("00001235%04d.jpg", i))
	}
	if k := rollKeys(noritsu); distinct(k) != 2 || k[0] != "R1-00123" {
		t.Errorf("prefix split: %v", k)
	}
	if k := rollKeys(digitsOnly); distinct(k) != 2 || k[0] != "00001234" {
		t.Errorf("digit-only split: %v", k)
	}

	// Things that are one roll, or not rolls at all.
	var camera, mixedCams, tiny []string
	for i := 4521; i < 4540; i++ {
		camera = append(camera, fmt.Sprintf("IMG_%d.JPG", i))
	}
	for i := 1; i <= 5; i++ {
		mixedCams = append(mixedCams, fmt.Sprintf("DSC_%04d.JPG", i), fmt.Sprintf("DSCF%04d.JPG", i))
	}
	tiny = []string{"a1.jpg", "b1.jpg"}
	for name, names := range map[string][]string{"camera": camera, "different cameras": mixedCams, "tiny": tiny} {
		if k := rollKeys(names); k != nil {
			t.Errorf("%s should not split: %v", name, k)
		}
	}
}

func TestFolderHelpers(t *testing.T) {
	for in, want := range map[string]string{"OneDrive_2025-12-09": "2025-12-09", "scans 20260613": "2026-06-13", "Lisbon trip": "", "Roll 2024-13-40": ""} {
		if got := folderDate(in); got != want {
			t.Errorf("folderDate(%q) = %q, want %q", in, got, want)
		}
	}
	for name, want := range map[string]bool{"OneDrive_2025-12-09": true, "B001738": true, "Downloads": true, "Lisbon trip": false, "Portra 400": false} {
		if got := genericFolder.MatchString(name); got != want {
			t.Errorf("genericFolder(%q) = %v", name, got)
		}
	}
}

// A lab download: one folder named after the download, holding two rolls of a Frontier order.
func TestOpenSourceSplitsLabDownload(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "OneDrive_2025-12-09")
	os.MkdirAll(src, 0o755)
	for _, n := range slices.Concat(frontierRoll("B001738", 1, 6, true), frontierRoll("B001738", 2, 5, false)) {
		os.WriteFile(filepath.Join(src, n), []byte("x"), 0o644)
	}
	a := NewApp(filepath.Join(root, "data"), false)

	// Without detection it stays one roll, but the download folder's name is still replaced by the order's.
	plain, err := a.OpenSource(filepath.ToSlash(src), true, false)
	if err != nil || plain.Rolls != 0 || len(plain.Groups) != 1 || plain.Groups[0].Count != 11 ||
		plain.Groups[0].Name != "B001738 roll 1" || plain.Groups[0].Detected {
		t.Fatalf("without detection: %+v %v", plain.Groups, err)
	}
	s, err := a.OpenSource(filepath.ToSlash(src), true, true)
	if err != nil || s.Rolls != 2 || len(s.Groups) != 2 {
		t.Fatalf("detected %d rolls, %d groups: %v", s.Rolls, len(s.Groups), err)
	}
	g := s.Groups[0]
	if g.Name != "B001738 roll 1" || g.Lab != "Order B001738, roll 1" || !g.Detected || g.Count != 6 || g.Date != "2025-12-09" {
		t.Errorf("first roll: %+v", g)
	}
	if s.Groups[1].Name != "B001738 roll 2" || s.Groups[1].Count != 5 {
		t.Errorf("second roll: %+v", s.Groups[1])
	}

	// Importing it makes one folder per roll.
	lib := filepath.Join(root, "lib")
	os.MkdirAll(lib, 0o755)
	a.settings.Update(func(st *Settings) { st.Libraries = []string{filepath.ToSlash(lib)} })
	req := ImportRequest{Source: s.Source, Subfolders: true, Detect: true, Split: true, Mode: "copy", Dest: filepath.ToSlash(lib), Structure: "{name}"}
	plans, err := a.PlanImport(req, s)
	if err != nil || len(plans) != 2 || plans[0].Dir != filepath.Join(lib, "B001738 roll 1") {
		t.Fatalf("%+v %v", plans, err)
	}
}

func TestRenameRoll(t *testing.T) {
	if _, err := exec.LookPath("exiftool"); err != nil {
		t.Skip("exiftool not installed")
	}
	root := t.TempDir()
	lib := filepath.Join(root, "lib")
	old := filepath.Join(lib, "OneDrive_2025-12-09")
	os.MkdirAll(old, 0o755)
	os.WriteFile(filepath.Join(old, "B001738-R1-00-36A.JPG"), jpegBytes(t), 0o644)
	a := NewApp(filepath.Join(root, "data"), false)
	a.lib.Scan([]string{filepath.ToSlash(lib)})

	dir, err := a.lib.RenameRoll(filepath.ToSlash(old), "2025-12-09 Kodak Gold 200")
	if err != nil {
		t.Fatal(err)
	}
	if r := a.lib.Roll(dir); r == nil || r.Count != 1 || r.Name != "2025-12-09 Kodak Gold 200" {
		t.Fatalf("renamed roll missing: %+v", r)
	}
	if a.lib.Roll(filepath.ToSlash(old)) != nil {
		t.Error("old roll still listed")
	}
	if _, err := a.lib.RenameRoll(dir, "bad/name"); err == nil {
		t.Error("a name with a slash should be refused")
	}
}
