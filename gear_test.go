package main

import (
	"os"
	"path/filepath"
	"testing"
)

func testGear(t *testing.T) *GearDB {
	root := t.TempDir()
	g := NewGearDB(root)
	dir := filepath.Join(g.Dir, "data", "canon")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "bodies.csv"), []byte(
		"slug,name,brand,type,mount,film_format,introduced,image,wikidata,commons_file,wikipedia,source\n"+
			"canon-ae-1,Canon AE-1,Canon,SLR,Canon FD lens mount,35mm,1976,images/canon/canon-ae-1.jpg,Q55674,,,wikidata\n"+
			"canon-f-1,Canon F-1,Canon,SLR,Canon FD lens mount,35mm,1971,,Q1,,,wikidata\n"+
			"pentax-67,Pentax 67,Pentax,SLR,Pentax 6x7 lens mount,120,1969,,Q2,,,wikidata\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "lenses.csv"), []byte(
		"slug,name,brand,mount,film_format,focal_length_mm,max_aperture,introduced,filter_mm,weight_g,image,wikidata,commons_file,wikipedia,source\n"+
			"canon-fd-50mm-f-1-4,Canon FD 50mm f/1.4,Canon,Canon FD lens mount,35mm,50,1.4,1971,55,370,,,,,wikipedia\n"), 0o644)
	return g
}

func TestGearReadsDatabase(t *testing.T) {
	g := testGear(t)
	items := g.Items()
	if len(items) != 4 {
		t.Fatalf("%+v", items)
	}
	ae1 := items[0]
	if ae1.Name != "Canon AE-1" || ae1.Format != "35mm" || ae1.Kind != "body" || ae1.Brand != "Canon" || ae1.Model != "AE-1" ||
		ae1.Image != "/geardb/images/canon/canon-ae-1.jpg" || ae1.Custom {
		t.Errorf("%+v", ae1)
	}
	if lens := g.Search(GearQuery{Kind: "lens", Q: "50"}, nil, 10); len(lens) != 1 || lens[0].Aperture != "1.4" || lens[0].Filter != "55" {
		t.Errorf("%+v", lens)
	}
}

func TestGearCustomAdditions(t *testing.T) {
	g := testGear(t)

	// Something the database doesn't have.
	own, err := g.Save(GearItem{Kind: "body", Name: "Nikon FM2", Mount: "Nikon F"})
	if err != nil || own.Slug != "nikon-fm2" || own.Brand != "Nikon" || own.Model != "FM2" || !own.Custom {
		t.Fatalf("%+v %v", own, err)
	}
	if got := g.Search(GearQuery{Kind: "body", Q: "fm2"}, nil, 10); len(got) != 1 || !got[0].Custom {
		t.Fatalf("custom gear not searchable: %+v", got)
	}

	// A second one with the same name gets its own slug.
	dup, _ := g.Save(GearItem{Kind: "body", Name: "Nikon FM2"})
	if dup.Slug != "nikon-fm2-2" {
		t.Errorf("slug clash: %q", dup.Slug)
	}

	// Editing a database entry keeps its slug and replaces it in the list.
	edited, err := g.Save(GearItem{Slug: "canon-ae-1", Kind: "body", Name: "Canon AE-1 (mine, black)", Mount: "Canon FD lens mount"})
	if err != nil {
		t.Fatal(err)
	}
	items := g.Items()
	if len(items) != 6 {
		t.Fatalf("%d items, expected the edit to replace the original", len(items))
	}
	found := false
	for _, it := range items {
		if it.Slug == "canon-ae-1" {
			found = it.Name == "Canon AE-1 (mine, black)" && it.Custom
		}
	}
	if !found {
		t.Error("edited entry missing")
	}

	// Photos live beside it.
	if err := g.SaveImage(edited.Slug, ".jpg", jpegBytes(t)); err != nil {
		t.Fatal(err)
	}
	if p := g.ImagePath("canon-ae-1"); p == "" {
		t.Error("no stored photo")
	}
	if err := g.SaveImage(edited.Slug, ".exe", []byte("x")); err == nil {
		t.Error("only images should be accepted")
	}

	// Forgetting your edit brings the original back.
	if err := g.Forget("canon-ae-1"); err != nil {
		t.Fatal(err)
	}
	for _, it := range g.Items() {
		if it.Slug == "canon-ae-1" && (it.Custom || it.Name != "Canon AE-1") {
			t.Errorf("original not restored: %+v", it)
		}
	}
	if g.ImagePath("canon-ae-1") != "" {
		t.Error("photo left behind")
	}
	if err := g.Forget("canon-f-1"); err == nil {
		t.Error("database entries can't be deleted")
	}
}

func TestGearSearchPrefersWhatYouUse(t *testing.T) {
	g := testGear(t)
	got := g.Search(GearQuery{Kind: "body", Q: "canon"}, map[string]int{"canon f-1": 3}, 10)
	if len(got) != 2 || got[0].Name != "Canon F-1" || got[0].Rolls != 3 {
		t.Fatalf("%+v", got)
	}
}

func TestGearFilters(t *testing.T) {
	g := testGear(t)
	facets := g.Facets("body")
	if got := facets["brands"]; len(got) != 2 || got[0] != "Canon" || got[1] != "Pentax" {
		t.Errorf("brands: %q", got)
	}
	if got := facets["mounts"]; len(got) != 2 || got[0] != "Canon FD" || got[1] != "Pentax 6x7" {
		t.Errorf("mounts: %q", got)
	}
	if got := facets["formats"]; len(got) != 2 || got[0] != "120" || got[1] != "35mm" {
		t.Errorf("formats: %q", got)
	}
	if got := g.Search(GearQuery{Kind: "body", Format: "120"}, nil, 10); len(got) != 1 || got[0].Name != "Pentax 67" {
		t.Errorf("filtering by film: %+v", got)
	}
	if got := g.Search(GearQuery{Kind: "body", Mount: "Canon FD"}, nil, 10); len(got) != 2 {
		t.Errorf("filtering by mount: %+v", got)
	}
	if got := g.Search(GearQuery{Brand: "pentax"}, nil, 10); len(got) != 1 {
		t.Errorf("filtering by brand: %+v", got)
	}
}
