// Emulsion: a film photography library backed by the Open Source Film Database.
//
// Desktop:  emulsion                      (native window; optional remote web UI in Settings)
// NAS:      emulsion -headless -addr 0.0.0.0:8080   (password via EMULSION_PASSWORD)
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// version is set at build time: go build -ldflags "-X main.version=1.2.3"
var version = "dev"

func logf(format string, args ...any) { log.Printf(format, args...) }

func defaultDataDir() string {
	if d, err := os.UserConfigDir(); err == nil {
		return filepath.Join(d, "Emulsion")
	}
	return "emulsion-data"
}

func main() {
	data := flag.String("data", defaultDataDir(), "data directory (settings, film database, thumbnail cache)")
	headless := flag.Bool("headless", false, "run only the web UI (NAS / server / Docker)")
	addr := flag.String("addr", "", "web UI address in headless mode (default: settings, else 0.0.0.0:8080)")
	library := flag.String("library", "", "headless: add this photo folder as a library")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("Emulsion", version)
		return
	}

	os.MkdirAll(*data, 0o755)
	if lf, err := os.OpenFile(filepath.Join(*data, "emulsion.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		log.SetOutput(io.MultiWriter(lf, os.Stderr)) // file first: GUI builds have no stderr and MultiWriter stops at the first error
	}

	gui = !*headless
	app := NewApp(*data, gui)
	if *headless {
		runHeadless(app, *addr, *library)
		return
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fatal(err)
	}
	go (&http.Server{Handler: app.Handler(true), ReadHeaderTimeout: 10 * time.Second}).Serve(ln)
	app.Start()
	app.ApplyRemote()
	url := "http://" + ln.Addr().String() + "/"
	logf("desktop UI on %s", url)
	openWindow(url, *data)
}

func runHeadless(app *App, addr, library string) {
	err := app.settings.Update(func(s *Settings) {
		if addr != "" {
			s.WebUI.Address = addr
		}
		s.WebUI.Enabled = true
		if pw := os.Getenv("EMULSION_PASSWORD"); pw != "" && !checkPassword(s.WebUI, pw) {
			s.WebUI.PasswordHash, s.WebUI.Salt = hashPassword(pw)
		}
		if library != "" {
			if abs, err := filepath.Abs(library); err == nil {
				s.Libraries = addUniq(s.Libraries, filepath.ToSlash(abs))
				if s.ImportTo == "" {
					s.ImportTo = s.Libraries[0]
				}
			}
		}
	})
	if err != nil {
		fatal(err)
	}
	s := app.settings.Get().WebUI
	if s.PasswordHash == "" && !isLoopbackHost(s.Address) {
		fatal(fmt.Errorf("refusing to serve %s without a password: set EMULSION_PASSWORD", s.Address))
	}
	app.Start()
	app.ApplyRemote()
	if e := app.remoteErr; e != "" {
		fatal(fmt.Errorf("web UI: %s", e))
	}
	logf("web UI on %v", lanURLs(s.Address))
	select {}
}

var gui bool // desktop mode: errors also get a dialog, since there's no console

func fatal(err error) {
	logf("fatal: %v", err)
	if gui {
		showError(err.Error())
	}
	os.Exit(1)
}
