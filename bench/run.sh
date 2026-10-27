#!/bin/sh
# Compares nib with Zed and Sublime Text on one file or folder (macOS only).
# Editors missing from /Applications are skipped.
# Usage: bench/run.sh <file> [runs] [timeout-seconds]
set -e
dir=$(cd "$(dirname "$0")" && pwd)
root=$(dirname "$dir")
runs=${2:-3}
timeout=${3:-120}
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

swiftc -O "$dir/bench.swift" -o "$work/bench"
(cd "$root" && go build -o "$work/nib" .)

# Zed gets its own data folder with language servers and updates off.
mkdir -p "$work/zed/config"
cat > "$work/zed/config/settings.json" <<'JSON'
{
  "telemetry": { "diagnostics": false, "metrics": false },
  "auto_update": false,
  "restore_on_startup": "none",
  "enable_language_server": false
}
JSON

zed=/Applications/Zed.app/Contents/MacOS/zed
# Sublime's safe mode uses a clean profile: no packages, no session.
subl="/Applications/Sublime Text.app/Contents/MacOS/sublime_text"

# One launch of each editor; prints a line prefixed with the editor's name.
nib() { echo "nib      $("$work/bench" "$timeout" "$work/nib" "$1")"; }
zed() { [ -x "$zed" ] && echo "zed      $("$work/bench" "$timeout" "$zed" --user-data-dir "$work/zed" "$1")"; }
sublime() { [ -x "$subl" ] && echo "sublime  $("$work/bench" "$timeout" "$subl" --safe-mode "$1")"; }

# Warm the file cache and the editors; the first launch is not counted.
[ -f "$1" ] && cat "$1" > /dev/null
{ nib "$1"; zed "$1"; sublime "$1"; } > /dev/null || true
for i in $(seq "$runs"); do
  nib "$1"
  zed "$1" || true
  sublime "$1" || true
done
