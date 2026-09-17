package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNewerVersion(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"0.1.1", "0.1.0", true},
		{"v0.2.0", "0.1.9", true},
		{"0.1.0", "0.1.0", false},
		{"0.1.0", "0.1.1", false},
		{"1.0.0", "1.0.0-beta.2", true},
		{"1.0.0-beta.2", "1.0.0", false},
		{"1.0.0-beta.2", "1.0.0-beta.1", true},
		{"0.10.0", "0.9.0", true},
	} {
		if got := newerVersion(c.a, c.b); got != c.want {
			t.Errorf("newerVersion(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// fakeRelease serves a GitHub-style release for this platform containing binary, optionally with a bad checksum.
func fakeRelease(t *testing.T, tag string, binary []byte, badSum bool) *httptest.Server {
	var archive bytes.Buffer
	name := fmt.Sprintf("emulsion-%s-%s-%s.tar.gz", strings.TrimPrefix(tag, "v"), runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		name = fmt.Sprintf("Emulsion-%s-windows-%s.zip", strings.TrimPrefix(tag, "v"), runtime.GOARCH)
		zw := zip.NewWriter(&archive)
		w, _ := zw.Create("Emulsion.exe")
		w.Write(binary)
		w, _ = zw.Create("README.md")
		w.Write([]byte("readme"))
		zw.Close()
	} else {
		gz := gzip.NewWriter(&archive)
		tw := tar.NewWriter(gz)
		tw.WriteHeader(&tar.Header{Name: "./emulsion", Mode: 0o755, Size: int64(len(binary)), Typeflag: tar.TypeReg})
		tw.Write(binary)
		tw.Close()
		gz.Close()
	}
	sum := sha256.Sum256(archive.Bytes())
	hexSum := hex.EncodeToString(sum[:])
	if badSum {
		hexSum = strings.Repeat("0", 64)
	}
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv.URL+"/releases/tag/"+tag, http.StatusFound)
	})
	mux.HandleFunc("/api/latest", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"tag_name": tag, "body": "notes"})
	})
	mux.HandleFunc("/releases/download/"+tag+"/"+name, func(w http.ResponseWriter, r *http.Request) { w.Write(archive.Bytes()) })
	mux.HandleFunc("/releases/download/"+tag+"/SHA256SUMS.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  other-file.tar.gz\n%s  %s\n", strings.Repeat("a", 64), hexSum, name)
	})
	releasesURL, releaseAPI = srv.URL+"/releases", srv.URL+"/api/latest"
	return srv
}

func withVersion(t *testing.T, v string) {
	old := version
	version = v
	t.Cleanup(func() { version = old })
}

func TestUpdateInstall(t *testing.T) {
	withVersion(t, "0.1.0")
	srv := fakeRelease(t, "v0.2.0", []byte("new build"), false)
	defer srv.Close()

	exe := filepath.Join(t.TempDir(), "Emulsion.exe")
	os.WriteFile(exe, []byte("old build"), 0o755)

	u := NewUpdater()
	info := u.Check()
	if !info.Available || !info.CanInstall || info.Latest != "0.2.0" || info.Notes != "notes" {
		t.Fatalf("%+v", info)
	}
	if err := u.installTo(u.rel, exe); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "new build" {
		t.Errorf("exe not replaced: %q", b)
	}
	if b, _ := os.ReadFile(exe + ".old"); string(b) != "old build" {
		t.Errorf("previous build not kept: %q", b)
	}
}

func TestUpdateRejectsBadChecksum(t *testing.T) {
	withVersion(t, "0.1.0")
	srv := fakeRelease(t, "v0.2.0", []byte("tampered"), true)
	defer srv.Close()

	exe := filepath.Join(t.TempDir(), "Emulsion.exe")
	os.WriteFile(exe, []byte("old build"), 0o755)
	u := NewUpdater()
	u.Check()
	if err := u.installTo(u.rel, exe); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("expected checksum error, got %v", err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "old build" {
		t.Error("a failed update must leave the current build in place")
	}
	if entries, _ := os.ReadDir(filepath.Dir(exe)); len(entries) != 1 {
		t.Errorf("leftover files: %v", entries)
	}
}

func TestUpdateNotOfferedForSameOrDevVersion(t *testing.T) {
	srv := fakeRelease(t, "v0.2.0", []byte("x"), false)
	defer srv.Close()

	withVersion(t, "0.2.0")
	if info := NewUpdater().Check(); info.Available {
		t.Errorf("same version offered as update: %+v", info)
	}
	version = "dev"
	if info := NewUpdater().Check(); info.CanInstall {
		t.Errorf("dev build should not self-update: %+v", info)
	}
}
