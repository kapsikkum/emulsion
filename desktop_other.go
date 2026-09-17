//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
)

// ponytail: no native window off Windows; opens the default browser. Add a webview when a Mac/Linux desktop build matters.
func openWindow(url, _ string) {
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	exec.Command(opener, url).Start()
	fmt.Println("Emulsion is running at", url, "— press Ctrl+C to quit")
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	<-c
}

func showError(msg string) { fmt.Fprintln(os.Stderr, msg) }

func hideWindow(*exec.Cmd) {}
