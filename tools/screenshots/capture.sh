#!/usr/bin/env bash
#
# Regenerate every README screenshot + the unlock GIF from anonymized MOCK data.
#
# It builds cmd/screenshots (behind the `screenshots` build tag), which runs an in-process
# mock of the WeLock API and composes each screen into the full main window, then captures
# each window focus-free (screencapture -l <windowID>) so it doesn't matter what else you're
# doing. A throwaway HOME is used, so your real session/config is never touched.
#
# macOS only. Needs: go, swift, screencapture, sips, ffmpeg (brew install ffmpeg).
# Grant the terminal Screen Recording permission (System Settings › Privacy & Security).
#
# Usage:  tools/screenshots/capture.sh
# Then review docs/screenshots/ + docs/unlock.gif and commit.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$SCRIPT_DIR/../.." && pwd)"
SHOTS="$REPO/docs/screenshots"
BIN=/tmp/welock-screens
FRAMES=/tmp/welock-frames
HOMEDIR=/tmp/welock-screens-home # throwaway HOME — the real session is left untouched

mkdir -p "$SHOTS" "$HOMEDIR/Library/Application Support"

echo "building screenshot tool…"
( cd "$REPO" && go build -tags "screenshots tinygobt" -o "$BIN" ./cmd/screenshots ) || exit 1

winid() { swift "$SCRIPT_DIR/winid.swift" welock-screens 2>/dev/null || true; }

cap() { # <mode> <outfile-basename>
  pkill -f welock-screens >/dev/null 2>&1; sleep 1
  HOME="$HOMEDIR" "$BIN" "$1" >/tmp/welock-screens.log 2>&1 &
  sleep 6
  local wid; wid="$(winid)"
  if [ -n "$wid" ]; then screencapture -x -o -l"$wid" "$SHOTS/$2.png" && echo "  $2"; else echo "  $2: NO WINDOW"; fi
  pkill -f welock-screens >/dev/null 2>&1
}

echo "capturing screens…"
cap login              01-login
cap locks              02-your-locks
cap remote             03-remote-control
cap manage             04-manage
cap bluetooth          05-bluetooth
cap "modal:people"     06-people-access
cap "modal:temp"       07-temporary-passwords
cap "modal:activity"   08-activity
cap "modal:creds"      09-gateway-credentials
cap "modal:addmember"  10-add-member
cap gateway            12-gateway
cap settings           13-settings
cap "modal:addchooser" 14-add-menu

echo "recording unlock GIF…"
rm -rf "$FRAMES"; mkdir -p "$FRAMES"
pkill -f welock-screens >/dev/null 2>&1; sleep 1
HOME="$HOMEDIR" "$BIN" unlockgif >/tmp/welock-screens.log 2>&1 &
sleep 1.6
wid="$(winid)"
for i in $(seq -w 1 16); do
  screencapture -x -o -l"$wid" "$FRAMES/f$i.png" 2>/dev/null
  sleep 0.4
done
pkill -f welock-screens >/dev/null 2>&1
if command -v ffmpeg >/dev/null 2>&1; then
  ffmpeg -y -framerate 3 -i "$FRAMES/f%02d.png" \
    -vf "scale=820:-1:flags=lanczos,split[s0][s1];[s0]palettegen=max_colors=96[p];[s1][p]paletteuse=dither=bayer:bayer_scale=3" \
    "$REPO/docs/unlock.gif" >/dev/null 2>&1 && echo "  docs/unlock.gif"
else
  echo "  (ffmpeg not found — skipping GIF; brew install ffmpeg)"
fi

echo "downscaling PNGs to 1400px…"
for f in "$SHOTS"/*.png; do sips -Z 1400 "$f" >/dev/null 2>&1; done

echo "done — review docs/screenshots/ + docs/unlock.gif, then commit."
