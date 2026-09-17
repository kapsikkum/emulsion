# Emulsion

A film photography library. Emulsion files your scans into rolls, tags them with the film stock and camera,
and keeps a local copy of the [Open Source Film Database](https://github.com/dxdatabase/Open-source-film-database)
that you can edit.

It runs as a **desktop app** (a native window on Windows) or **headless on a NAS** with the same web UI,
the way qBittorrent does. The desktop app can also turn on a password-protected web UI for other devices.

## What it does

- **Library.** Every folder of images is a roll. Rolls are grouped by year, with film, camera, lens and ISO.
- **Viewer.** Zoom with the scroll wheel, a pinch, a double-click or +/−, and drag to pan. Fit and 1:1 are one
  key away (`0`, `1`), and zooming past the preview loads the full-resolution file. Rotate with `R` and save the
  rotation into the photo's EXIF orientation, or into the XMP sidecar for RAW files. The info panel (`I`) shows
  film, camera, lens, ISO, date, size and every metadata tag. A filmstrip and the arrow keys move between frames.
- **Metadata in the files.** Film, camera, lens, ISO and date are written as EXIF/XMP, so Lightroom and
  other apps see them. The film is stored as the keyword `film:<name>`. Camera RAW files get a Lightroom-style
  `.xmp` sidecar, and the RAW itself is never modified.
- **Lightroom-style import.** Import from a folder, a memory card or a lab `.zip`. You can also upload from
  the browser or drop files onto the page. Pick frames, skip duplicates, and choose copy, move or add in place.
  A folder template such as `{yyyy}/{date} {name}` files the rolls, with optional renaming (`{name}_{seq}`).
  Metadata is applied on the way in. Lab zips with several roll folders become several rolls, each with its own
  editable name, film, ISO and date. Emulsion suggests tidy names ("Roll 1 - Portra 400" becomes "Portra 400")
  and guesses the film from the folder name. You choose the naming style.
- **Hot folder.** Save lab zips into a watched folder and they import on their own. The zips are then moved
  to `Imported/`.
- **Films in production first.** The database marks which films are still on the market, so the Films page has
  an **In production** tab, and searches list current stocks ahead of discontinued variants. Everyday stocks lead
  (Portra, Gold, UltraMax, ColorPlus, HP5, C200…). You can change a film's availability when you edit it.
- **Film database.** The database is a git clone. Your edits are local commits, and updates are
  `git pull` with your edits kept. You can see and revert your edits in Settings.
- **Updates.** Emulsion checks GitHub daily for a new release and shows it in the sidebar. **Install and restart**
  downloads the build for your platform, verifies it against the release's `SHA256SUMS.txt`, replaces the
  program and restarts. The previous version is kept as `Emulsion.exe.old` until the next start. You can turn
  off daily checks in Settings. Docker installs update by pulling the new image.
- **Open in.** Opens a roll in Lightroom Classic, Photoshop, NegPy or your own editors. Emulsion finds
  common installs, and you can override paths in Settings.
- **NegPy.** Set NegPy's export to a sub-folder of the source called `export` and turn on copying metadata.
  The positives then show inside their roll, with its film and camera. Film stocks you set in NegPy's
  metadata panel (`negpy:CaptureFilmStock`) are read too. NegPy can't be passed files, so "Open in NegPy"
  opens NegPy and the roll folder, and you drag the folder in.

## Requirements

- [Git](https://git-scm.com/) and [ExifTool](https://exiftool.org/) on the `PATH`
  (`winget install Git.Git OliverBetz.ExifTool`).
- Windows 10/11 for the native window. It uses the WebView2 runtime that ships with Windows. On macOS
  and Linux, Emulsion opens in your browser instead.

## Run

```bash
go run .
```

Build the desktop app with its icon:

```bash
go build -ldflags "-H windowsgui -s -w" -o Emulsion.exe .
```

### Where data is kept

Your photos stay where they are, and their metadata lives inside them. Everything else goes in the data
folder: `%AppData%\Emulsion` on Windows, `~/Library/Application Support/Emulsion` on macOS, and
`~/.config/Emulsion` on Linux. Pass `-data <dir>` to use a different one.

| Path | What it is |
| --- | --- |
| `filmdb/` | Git clone of the Open Source Film Database: `film_database.csv`, box images, and your edits as commits |
| `settings.json` | Libraries, import templates, hot folder, app paths, remote-access password hash |
| `library.json` | Cache of the last photo scan, rebuilt from your files |
| `imported.json` | Names and sizes of imported source files, used to spot duplicates |
| `thumbs/` | Thumbnail cache, safe to delete |
| `staging/` | Unpacked zips and browser uploads, cleaned up after a week |
| `webview/` | Desktop window's browser profile |
| `emulsion.log` | Log |

### NAS / server

```bash
docker build -t emulsion .
```

```bash
docker run -d -p 8080:8080 -e EMULSION_PASSWORD=change-me -v emulsion-data:/data -v /volume1/photos/film:/photos emulsion
```

Or without Docker:

```bash
EMULSION_PASSWORD=change-me emulsion -headless -addr 0.0.0.0:8080 -library /volume1/photos/film
```

Emulsion won't listen beyond localhost without a password. For access away from home, use a VPN such as
Tailscale or an HTTPS reverse proxy.

## Develop

```bash
go test ./...
```

The UI is plain HTML, CSS and JavaScript in `web/`, embedded into the binary, so there's no build step.
After changing `winres/`, rebuild the Windows resources with `go-winres make --arch amd64`.

## Releases

Versions follow [semantic versioning](https://semver.org/). To publish one, tag a commit and push the tag:

```bash
git tag v0.1.0
```

```bash
git push origin v0.1.0
```

The Release workflow then runs the tests and builds:

- `Emulsion-<version>-windows-amd64.zip`: the desktop app, with the version in the `.exe` details
- `emulsion-<version>-{linux,darwin}-{amd64,arm64}.tar.gz`, plus `SHA256SUMS.txt`
- a GitHub release with notes generated from merged PRs (tags with a `-`, such as `v0.2.0-beta.1`,
  are marked as pre-releases)
- a multi-arch Docker image, `ghcr.io/<owner>/<repo>:<version>` and `:latest`

`emulsion -version` prints the version, and it also shows under Settings → Updates. Local builds say `dev`
unless you pass `-ldflags "-X main.version=1.2.3"`.

CI runs formatting, vet and tests on Linux, Windows and macOS for every push and pull request, and
Dependabot keeps Go modules, Actions and the Docker base image up to date.

## Credits

The film data and box images come from the Open Source Film Database by dxdatabase, licensed under
[CC BY-SA 4.0](https://creativecommons.org/licenses/by-sa/4.0/). The box image copyright belongs to
their respective owners.
