# Emulsion

A film photography library. Emulsion files your scans into rolls, tags them with the film stock and camera,
and keeps a local copy of the [Open Source Film Database](https://github.com/dxdatabase/Open-source-film-database)
that you can edit.

It runs as a **desktop app** (a native window on Windows) or **headless on a NAS** with the same web UI,
the way qBittorrent does. The desktop app can also turn on a password-protected web UI for other devices.

## What it does

- **Library.** Every folder of images is a roll. Rolls are grouped by year, with film, camera, lens and ISO.
  A lightbox shows each frame.
- **Metadata in the files.** Film, camera, lens, ISO and date are written as EXIF/XMP, so Lightroom and
  other apps see them. The film is stored as the keyword `film:<name>`. Camera RAW files get a Lightroom-style
  `.xmp` sidecar, and the RAW itself is never modified.
- **Lightroom-style import.** Import from a folder, a memory card or a lab `.zip`. You can also upload from
  the browser or drop files onto the page. Pick frames, skip duplicates, and choose copy, move or add in place.
  A folder template such as `{yyyy}/{date} {name}` files the rolls, with optional renaming (`{name}_{seq}`).
  Metadata is applied on the way in. Lab zips with several roll folders become several rolls.
- **Hot folder.** Save lab zips into a watched folder and they import on their own. The zips are then moved
  to `Imported/`.
- **Film database.** The database is a git clone. Your edits are local commits, and updates are
  `git pull` with your edits kept. You can see and revert your edits in Settings.
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

Data (settings, the film database clone, thumbnails, import history and the log) is stored in
`%AppData%\Emulsion` on Windows. Use `-data <dir>` to put it somewhere else.

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

`emulsion -version` prints the version, and it also shows under Settings → System. Local builds say `dev`
unless you pass `-ldflags "-X main.version=1.2.3"`.

CI runs formatting, vet and tests on Linux, Windows and macOS for every push and pull request, and
Dependabot keeps Go modules, Actions and the Docker base image up to date.

## Credits

The film data and box images come from the Open Source Film Database by dxdatabase, licensed under
[CC BY-SA 4.0](https://creativecommons.org/licenses/by-sa/4.0/). The box image copyright belongs to
their respective owners.
