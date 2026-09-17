package main

// "Open in" integrations: Lightroom Classic, Photoshop, NegPy and user-added editors.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type AppInfo struct {
	ID, Name, Path string
	Found          bool
	TakesFiles     bool   // false: the app can't be handed files, so Emulsion shows the folder too
	Note           string `json:",omitempty"`
}

var builtinApps = []AppInfo{
	{ID: "lightroom", Name: "Lightroom Classic", TakesFiles: true},
	{ID: "photoshop", Name: "Photoshop", TakesFiles: true},
	{ID: "negpy", Name: "NegPy", Note: "NegPy can't be handed files, so Emulsion opens it next to the roll's folder. Drag the folder into NegPy."},
}

// Apps lists integrations with their resolved executables.
func (a *App) Apps() []AppInfo {
	s := a.settings.Get()
	var out []AppInfo
	for _, app := range builtinApps {
		if p := s.Apps[app.ID]; p != "" {
			app.Path = p
		} else {
			app.Path = detectApp(app.ID)
		}
		app.Found = exists(app.Path)
		out = append(out, app)
	}
	for _, e := range s.Editors {
		out = append(out, AppInfo{ID: "editor:" + e.Name, Name: e.Name, Path: e.Path, Found: exists(e.Path), TakesFiles: true})
	}
	return out
}

func exists(p string) bool {
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

// OpenIn opens a roll folder (or specific files) in an app, or reveals it when id is "folder".
func (a *App) OpenIn(id, dir string, files []string) error {
	dir = filepath.FromSlash(dir)
	for i := range files {
		files[i] = filepath.FromSlash(files[i])
	}
	if id == "folder" {
		return reveal(dir, files)
	}
	for _, app := range a.Apps() {
		if app.ID != id {
			continue
		}
		if !app.Found {
			return errors.New(app.Name + " wasn't found. Set where it's installed in Settings → Integrations.")
		}
		if !app.TakesFiles {
			if err := launch(app.Path); err != nil {
				return err
			}
			return reveal(dir, nil)
		}
		args := files
		if len(args) == 0 || len(strings.Join(args, " ")) > 24000 { // stay under Windows' command-line limit
			args = []string{dir}
		}
		return launch(app.Path, args...)
	}
	return errors.New("unknown app")
}
