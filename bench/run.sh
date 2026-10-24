#!/bin/sh
# Compares quod and Zed on one file (macOS only).
# Usage: bench/run.sh <file> [runs] [timeout-seconds]
set -e
dir=$(cd "$(dirname "$0")" && pwd)
root=$(dirname "$dir")
runs=${2:-3}
timeout=${3:-120}
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

swiftc -O "$dir/bench.swift" -o "$work/bench"
(cd "$root" && go build -o "$work/quod" .)

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

# Warm the file cache and both editors; the first launch is not counted.
[ -f "$1" ] && cat "$1" > /dev/null
"$work/bench" "$timeout" "$work/quod" "$1" > /dev/null
"$work/bench" "$timeout" /Applications/Zed.app/Contents/MacOS/zed --user-data-dir "$work/zed" "$1" > /dev/null
for i in $(seq "$runs"); do
  echo "quod  $("$work/bench" "$timeout" "$work/quod" "$1")"
  echo "zed   $("$work/bench" "$timeout" /Applications/Zed.app/Contents/MacOS/zed --user-data-dir "$work/zed" "$1")"
done
