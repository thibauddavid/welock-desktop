# Screenshot harness

Regenerates every README screenshot in [`docs/screenshots/`](../../docs/screenshots/) and
the unlock GIF ([`docs/unlock.gif`](../../docs/unlock.gif)) from **anonymized mock data** —
no real account, no real lock, no network.

## One command

```sh
tools/screenshots/capture.sh
```

Then review `docs/screenshots/` + `docs/unlock.gif` and commit.

## How it works

- **`cmd/screenshots`** (built with `-tags "screenshots tinygobt"`) runs an in-process mock
  of the WeLock API — the `payloads` map in [`cmd/screenshots/main.go`](../../cmd/screenshots/main.go)
  is where the fake locks, people, credentials and activity live. Edit it to change what the
  screenshots show. It signs in with a fake token (`Core.LoginWithToken`) against that mock,
  then composes each screen into the full main window via the preview helpers in
  [`internal/ui/preview.go`](../../internal/ui/preview.go) (also `screenshots`-tagged, so
  none of this compiles into the shipped app).
- **`capture.sh`** builds that tool, launches it once per screen under a throwaway `HOME`
  (your real session/config is never touched), and captures each window **focus-free** with
  `screencapture -l <windowID>` — so it doesn't matter what you're doing on the machine
  while it runs. `winid.swift` resolves the window id without raising the window.
- The unlock GIF is a short frame burst of the `unlockgif` mode (which auto-taps Unlock and
  polls a stateful mock so it shows Ready → relaying → confirmed), assembled with `ffmpeg`.
- Finally every PNG is downscaled to 1400px with `sips`.

## Requirements (macOS)

`go`, `swift`, `screencapture`, `sips` (all stock on macOS) and `ffmpeg`
(`brew install ffmpeg`, only for the GIF). Grant your terminal **Screen Recording**
permission in System Settings › Privacy & Security.

## Running a single screen

```sh
go build -tags "screenshots tinygobt" -o /tmp/welock-screens ./cmd/screenshots
HOME=/tmp/throwaway /tmp/welock-screens remote        # feature: locks|remote|manage|bluetooth|gateway|settings
HOME=/tmp/throwaway /tmp/welock-screens modal:people  # a modal composed over the window
HOME=/tmp/throwaway /tmp/welock-screens unlockgif     # auto-taps Unlock for the GIF
```
