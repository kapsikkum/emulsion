package main

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Settings struct {
	Libraries   []string `json:"libraries"`
	ImportTo    string   `json:"importTo"`
	UpdateHours int      `json:"updateHours"`
	WebUI       WebUI    `json:"webui"`

	Structure  string            `json:"structure"`  // import folder template, e.g. "{yyyy}/{date} {name}"
	Rename     string            `json:"rename"`     // import file name template; "" keeps original names
	ExportDirs []string          `json:"exportDirs"` // sub-folders holding converted positives (NegPy exports)
	HotFolder  HotFolder         `json:"hotFolder"`
	Apps       map[string]string `json:"apps"`    // app id -> executable, overriding auto-detection
	Editors    []Editor          `json:"editors"` // extra "Open in" apps
}

type HotFolder struct {
	Enabled bool   `json:"enabled"`
	Path    string `json:"path"`
}

type Editor struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

var defaultExportDirs = []string{"export", "exports", "positives", "converted", "negpy"}

type WebUI struct {
	Enabled      bool   `json:"enabled"`
	Address      string `json:"address"`
	PasswordHash string `json:"passwordHash,omitempty"`
	Salt         string `json:"salt,omitempty"`
}

type Store struct {
	file string
	mu   sync.RWMutex
	s    Settings
}

func LoadSettings(dataDir string) *Store {
	st := &Store{file: filepath.Join(dataDir, "settings.json")}
	st.s = Settings{UpdateHours: 24, WebUI: WebUI{Address: "0.0.0.0:8080"}}
	if b, err := os.ReadFile(st.file); err == nil {
		json.Unmarshal(b, &st.s)
	}
	if st.s.Structure == "" {
		st.s.Structure = "{name}"
	}
	if st.s.ExportDirs == nil {
		st.s.ExportDirs = defaultExportDirs
	}
	return st
}

func (st *Store) Get() Settings {
	st.mu.RLock()
	defer st.mu.RUnlock()
	s := st.s
	// Copies, and never nil: the UI reads these as JSON arrays and objects, and a fresh install has none yet.
	s.Libraries = append([]string{}, s.Libraries...)
	s.ExportDirs = append([]string{}, s.ExportDirs...)
	s.Editors = append([]Editor{}, s.Editors...)
	s.Apps = maps.Clone(s.Apps)
	if s.Apps == nil {
		s.Apps = map[string]string{}
	}
	return s
}

func (st *Store) Update(f func(*Settings)) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	f(&st.s)
	b, _ := json.MarshalIndent(st.s, "", "  ")
	if err := os.WriteFile(st.file+".tmp", b, 0o600); err != nil {
		return err
	}
	return os.Rename(st.file+".tmp", st.file)
}

func hashPassword(pw string) (hash, salt string) {
	s := make([]byte, 16)
	rand.Read(s)
	k, _ := pbkdf2.Key(sha256.New, pw, s, 600_000, 32)
	return hex.EncodeToString(k), hex.EncodeToString(s)
}

func checkPassword(w WebUI, pw string) bool {
	s, err := hex.DecodeString(w.Salt)
	if err != nil || w.PasswordHash == "" {
		return false
	}
	k, _ := pbkdf2.Key(sha256.New, pw, s, 600_000, 32)
	return subtle.ConstantTimeCompare([]byte(hex.EncodeToString(k)), []byte(w.PasswordHash)) == 1
}

// Sessions are in-memory: restarting the app logs remote users out.
type Sessions struct {
	mu sync.Mutex
	m  map[string]time.Time
}

const sessionTTL = 30 * 24 * time.Hour

func (s *Sessions) New() string {
	b := make([]byte, 32)
	rand.Read(b)
	tok := hex.EncodeToString(b)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[string]time.Time{}
	}
	s.m[tok] = time.Now().Add(sessionTTL)
	return tok
}

func (s *Sessions) Valid(tok string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.m[tok]
	if ok && time.Now().After(exp) {
		delete(s.m, tok)
		return false
	}
	return ok
}

func (s *Sessions) Delete(tok string) { s.mu.Lock(); delete(s.m, tok); s.mu.Unlock() }

func (s *Sessions) Clear() { s.mu.Lock(); s.m = nil; s.mu.Unlock() }
