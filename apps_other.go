//go:build !windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
)

var appGlobs = map[string][]string{
	"lightroom": {"/Applications/Adobe Lightroom Classic/Adobe Lightroom Classic.app"},
	"photoshop": {"/Applications/Adobe Photoshop */Adobe Photoshop *.app"},
	"negpy":     {"/Applications/NegPy.app", os.ExpandEnv("$HOME/Applications/NegPy*.AppImage"), "/opt/NegPy*/NegPy*"},
}

func detectApp(id string) string {
	for _, g := range appGlobs[id] {
		if matches, _ := filepath.Glob(g); len(matches) > 0 {
			slices.Sort(matches)
			return matches[len(matches)-1]
		}
	}
	return ""
}

func launch(app string, args ...string) error {
	if runtime.GOOS == "darwin" && filepath.Ext(app) == ".app" {
		return exec.Command("open", append([]string{"-a", app}, args...)...).Start()
	}
	return exec.Command(app, args...).Start()
}

func reveal(dir string, files []string) error {
	if runtime.GOOS == "darwin" {
		if len(files) > 0 {
			return exec.Command("open", "-R", files[0]).Start()
		}
		return exec.Command("open", dir).Start()
	}
	return exec.Command("xdg-open", dir).Start()
}
