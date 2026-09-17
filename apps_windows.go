package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
)

var appGlobs = map[string][]string{
	"lightroom": {`%ProgramFiles%\Adobe\Adobe Lightroom Classic*\Lightroom.exe`},
	"photoshop": {`%ProgramFiles%\Adobe\Adobe Photoshop*\Photoshop.exe`},
	"negpy":     {`%ProgramFiles%\NegPy\NegPy.exe`, `%LOCALAPPDATA%\Programs\NegPy\NegPy.exe`, `%ProgramFiles(x86)%\NegPy\NegPy.exe`},
}

// detectApp finds an install in the usual places, preferring the newest versioned folder.
func detectApp(id string) string {
	for _, g := range appGlobs[id] {
		matches, _ := filepath.Glob(os.ExpandEnv(strings.NewReplacer("%ProgramFiles%", "${ProgramFiles}", "%LOCALAPPDATA%", "${LOCALAPPDATA}", "%ProgramFiles(x86)%", "${ProgramFiles(x86)}").Replace(g)))
		if len(matches) > 0 {
			slices.Sort(matches)
			return matches[len(matches)-1]
		}
	}
	return ""
}

func launch(exe string, args ...string) error {
	cmd := exec.Command(exe, args...)
	cmd.Dir = filepath.Dir(exe)
	return cmd.Start()
}

// reveal opens Explorer on a folder, selecting the first file when given.
func reveal(dir string, files []string) error {
	cmd := exec.Command("explorer.exe")
	// Explorer needs /select,"path" unquoted as one token, which Go's argument quoting would break.
	if len(files) > 0 {
		cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `explorer.exe /select,"` + files[0] + `"`}
	} else {
		cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `explorer.exe "` + dir + `"`}
	}
	return cmd.Start()
}
