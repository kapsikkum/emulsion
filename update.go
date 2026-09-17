package main

// Self-update from GitHub releases: check the latest release, download this platform's build,
// verify it against the release's SHA256SUMS.txt, swap the executable and restart.

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	// github.com's /releases/latest redirect and download URLs aren't rate limited per IP, unlike the API.
	releasesURL = "https://github.com/kapsikkum/emulsion/releases"
	releaseAPI  = "https://api.github.com/repos/kapsikkum/emulsion/releases/latest" // only for release notes
)

const maxDownload = 256 << 20

type UpdateInfo struct {
	Current, Latest, Notes, URL string
	Available, CanInstall       bool
	Reason                      string `json:",omitempty"` // why installing isn't possible here
	Checking, Installing        bool
	Progress                    int
	Error                       string `json:",omitempty"`
	Checked                     time.Time
}

type release struct {
	Tag, Notes, URL string
}

type Updater struct {
	mu      sync.Mutex
	info    UpdateInfo
	rel     *release
	restart func(exe string) error // replaced in tests
	// beforeRestart releases the web UI port so the new version can bind it straight away.
	beforeRestart func()
}

func NewUpdater() *Updater {
	return &Updater{info: UpdateInfo{Current: version}, restart: restartInto}
}

func (u *Updater) Info() UpdateInfo {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.info
}

func (u *Updater) set(f func(*UpdateInfo)) { u.mu.Lock(); f(&u.info); u.mu.Unlock() }

var httpClient = &http.Client{Timeout: 10 * time.Minute}

func get(url string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Emulsion/"+version)
	if strings.HasPrefix(url, releaseAPI) {
		req.Header.Set("Accept", "application/vnd.github+json")
	}
	resp, err := httpClient.Do(req)
	if err == nil && resp.StatusCode != 200 {
		resp.Body.Close()
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return resp, err
}

// Check finds the latest release.
func (u *Updater) Check() UpdateInfo {
	u.set(func(i *UpdateInfo) { i.Checking, i.Error = true, "" })
	rel, err := latestRelease()
	u.mu.Lock()
	defer u.mu.Unlock()
	u.info.Checking, u.info.Checked = false, time.Now()
	if err != nil {
		u.info.Error = "Couldn't check for updates: " + err.Error()
		return u.info
	}
	u.rel = rel
	u.info.Latest = strings.TrimPrefix(rel.Tag, "v")
	u.info.Notes, u.info.URL = rel.Notes, rel.URL
	u.info.Available = newerVersion(u.info.Latest, version)
	u.info.CanInstall, u.info.Reason = canInstall(rel)
	return u.info
}

// latestRelease reads the newest tag from the /releases/latest redirect, then adds notes from the API when it answers.
func latestRelease() (*release, error) {
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequest("GET", releasesURL+"/latest", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Emulsion/"+version)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	resp.Body.Close()
	loc := resp.Header.Get("Location")
	i := strings.LastIndex(loc, "/tag/")
	if resp.StatusCode/100 != 3 || i < 0 {
		return nil, fmt.Errorf("no published release found (%s)", resp.Status)
	}
	rel := &release{Tag: loc[i+len("/tag/"):], URL: loc}
	if resp, err := get(releaseAPI); err == nil { // best effort: the API is rate limited
		var r struct {
			TagName string `json:"tag_name"`
			Body    string `json:"body"`
		}
		if json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&r) == nil && r.TagName == rel.Tag {
			rel.Notes = r.Body
		}
		resp.Body.Close()
	}
	return rel, nil
}

func canInstall(rel *release) (bool, string) {
	switch {
	case version == "dev":
		return false, "This is a development build. Build or download a release to update."
	case inContainer():
		return false, "Running in Docker. Pull the new image to update."
	case assetName(rel.Tag) == "":
		return false, fmt.Sprintf("Releases don't include a build for %s/%s.", runtime.GOOS, runtime.GOARCH)
	}
	return true, ""
}

func inContainer() bool {
	_, err := os.Stat("/.dockerenv")
	return err == nil || os.Getenv("container") != ""
}

// assetName is this platform's archive in a release, as named by .github/workflows/release.yml.
func assetName(tag string) string {
	v := strings.TrimPrefix(tag, "v")
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "windows/amd64":
		return "Emulsion-" + v + "-windows-amd64.zip"
	case "linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64":
		return fmt.Sprintf("emulsion-%s-%s-%s.tar.gz", v, runtime.GOOS, runtime.GOARCH)
	}
	return ""
}

func downloadURL(tag, name string) string { return releasesURL + "/download/" + tag + "/" + name }

// newerVersion reports whether a is a later semantic version than b ("1.2.0" > "1.2.0-beta.1" > "1.1.9").
func newerVersion(a, b string) bool {
	parse := func(v string) ([3]int, string) {
		v = strings.TrimPrefix(v, "v")
		core, pre, _ := strings.Cut(v, "-")
		var n [3]int
		for i, p := range strings.SplitN(core, ".", 3) {
			n[i], _ = strconv.Atoi(p)
		}
		return n, pre
	}
	na, pa := parse(a)
	nb, pb := parse(b)
	if na != nb {
		for i := range na {
			if na[i] != nb[i] {
				return na[i] > nb[i]
			}
		}
	}
	switch {
	case pa == pb:
		return false
	case pa == "":
		return true
	case pb == "":
		return false
	}
	return pa > pb
}

// Start begins installing the latest release in the background. Installing is already true when it returns.
func (u *Updater) Start() error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.info.Installing {
		return errors.New("an update is already installing")
	}
	if u.rel == nil || !u.info.Available || !u.info.CanInstall {
		return errors.New("no installable update; check for updates first")
	}
	u.info.Installing, u.info.Progress, u.info.Error = true, 0, ""
	go u.install(u.rel)
	return nil
}

// install downloads, verifies and swaps in the release, then restarts.
func (u *Updater) install(rel *release) {
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err == nil {
		err = u.installTo(rel, exe)
	}
	if err == nil {
		logf("updated to %s, restarting", rel.Tag)
		if u.beforeRestart != nil {
			u.beforeRestart()
		}
		err = u.restart(exe)
	}
	if err != nil {
		logf("update failed: %v", err)
		u.set(func(i *UpdateInfo) { i.Installing, i.Error = false, "Update failed: "+err.Error() })
	}
}

func (u *Updater) installTo(rel *release, exe string) error {
	name := assetName(rel.Tag)
	if name == "" {
		return errors.New("no build for this platform")
	}
	url := downloadURL(rel.Tag, name)
	want, err := checksumFor(downloadURL(rel.Tag, "SHA256SUMS.txt"), name)
	if err != nil {
		return err
	}

	dir := filepath.Dir(exe)
	archive, err := os.CreateTemp(dir, ".emulsion-download-*")
	if err != nil {
		return fmt.Errorf("can't write next to %s: %w", exe, err)
	}
	defer os.Remove(archive.Name())
	defer archive.Close()
	if err := u.download(url, archive); err != nil {
		return err
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return err
	}
	h := sha256.New()
	if _, err := io.Copy(h, archive); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fmt.Errorf("checksum mismatch for %s (got %s, want %s)", name, got, want)
	}

	next := exe + ".new"
	if err := extractBinary(archive.Name(), name, next); err != nil {
		os.Remove(next)
		return err
	}
	// A running executable can't be overwritten on Windows, but it can be renamed.
	old := exe + ".old"
	os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		os.Remove(next)
		return err
	}
	if err := os.Rename(next, exe); err != nil {
		os.Rename(old, exe) // put the working version back
		return err
	}
	return nil
}

func (u *Updater) download(url string, to io.Writer) error {
	resp, err := get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	total := resp.ContentLength
	var done int64
	buf := make([]byte, 256<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if done += int64(n); done > maxDownload {
				return errors.New("download is unexpectedly large")
			}
			if _, err := to.Write(buf[:n]); err != nil {
				return err
			}
			if total > 0 {
				u.set(func(i *UpdateInfo) { i.Progress = int(100 * done / total) })
			}
		}
		if rerr == io.EOF {
			return nil
		}
		if rerr != nil {
			return rerr
		}
	}
}

func checksumFor(url, name string) (string, error) {
	resp, err := get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(b), "\n") {
		if f := strings.Fields(line); len(f) == 2 && strings.TrimPrefix(f[1], "*") == name && len(f[0]) == 64 {
			return strings.ToLower(f[0]), nil
		}
	}
	return "", fmt.Errorf("no checksum for %s in the release", name)
}

// extractBinary copies the Emulsion executable out of a release archive.
func extractBinary(archive, name, to string) error {
	var src io.Reader
	if strings.HasSuffix(name, ".zip") {
		zr, err := zip.OpenReader(archive)
		if err != nil {
			return err
		}
		defer zr.Close()
		for _, f := range zr.File {
			if strings.EqualFold(filepath.Base(f.Name), "Emulsion.exe") {
				rc, err := f.Open()
				if err != nil {
					return err
				}
				defer rc.Close()
				src = rc
				break
			}
		}
	} else {
		f, err := os.Open(archive)
		if err != nil {
			return err
		}
		defer f.Close()
		gz, err := gzip.NewReader(f)
		if err != nil {
			return err
		}
		tr := tar.NewReader(gz)
		for {
			h, err := tr.Next()
			if err != nil {
				break
			}
			if h.Typeflag == tar.TypeReg && filepath.Base(h.Name) == "emulsion" {
				src = tr
				break
			}
		}
	}
	if src == nil {
		return errors.New("the release archive doesn't contain the Emulsion executable")
	}
	out, err := os.OpenFile(to, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, io.LimitReader(src, maxDownload)); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// restartInto starts the new executable with the same arguments and exits this one.
func restartInto(exe string) error {
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Env = append(os.Environ(), "EMULSION_UPDATED_FROM="+version)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() {
		time.Sleep(300 * time.Millisecond) // let the install request's response go out
		os.Exit(0)
	}()
	return nil
}

// CleanOld removes the previous executable left by an update, once it has exited.
func CleanOld() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	for range 20 {
		if err := os.Remove(exe + ".old"); err == nil || os.IsNotExist(err) {
			return
		}
		time.Sleep(3 * time.Second)
	}
}
