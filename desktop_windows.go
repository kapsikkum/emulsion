package main

import (
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
)

// openWindow shows the UI in a native WebView2 window; closing it quits.
func openWindow(url, dataDir string) {
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		AutoFocus: true,
		DataPath:  filepath.Join(dataDir, "webview"),
		WindowOptions: webview2.WindowOptions{
			Title: "Emulsion", Width: 1360, Height: 880, Center: true, IconId: 1,
		},
	})
	if w == nil {
		// No WebView2 runtime: fall back to the default browser.
		exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
		showError("Microsoft Edge WebView2 is missing, so Emulsion opened in your browser.\nClose this box to quit Emulsion.")
		return
	}
	defer w.Destroy()
	w.Navigate(url)
	w.Run()
}

func showError(msg string) {
	user32 := syscall.NewLazyDLL("user32.dll")
	t, _ := syscall.UTF16PtrFromString("Emulsion")
	m, _ := syscall.UTF16PtrFromString(msg)
	user32.NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), 0x10)
}

// hideWindow stops git/exiftool flashing console windows in the GUI build.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
