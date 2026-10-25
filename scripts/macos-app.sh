#!/bin/sh
# Builds a universal (Apple Silicon + Intel) quod.app and quod binary.
# Usage: scripts/macos-app.sh [version] [outdir]
set -eu
version=${1:-dev}
out=${2:-dist}
ldflags="-s -w -X main.version=$version"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$out"

for arch in arm64 amd64; do
	GOOS=darwin GOARCH=$arch CGO_ENABLED=1 go build -trimpath -ldflags "$ldflags" -o "$work/quod-$arch" .
done
lipo -create -output "$out/quod" "$work/quod-arm64" "$work/quod-amd64"

app="$out/quod.app"
rm -rf "$app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
cp "$out/quod" "$app/Contents/MacOS/quod"

iconset="$work/quod.iconset"
mkdir "$iconset"
for s in 16 32 128 256 512; do
	sips -z $s $s appicon.png --out "$iconset/icon_${s}x${s}.png" >/dev/null
	sips -z $((s * 2)) $((s * 2)) appicon.png --out "$iconset/icon_${s}x${s}@2x.png" >/dev/null
done
iconutil -c icns -o "$app/Contents/Resources/quod.icns" "$iconset"

short=${version#v}
case $short in [0-9]*) ;; *) short=0.0.0 ;; esac
cat >"$app/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleName</key>
	<string>quod</string>
	<key>CFBundleExecutable</key>
	<string>quod</string>
	<key>CFBundleIdentifier</key>
	<string>io.github.hipjim.quod</string>
	<key>CFBundleIconFile</key>
	<string>quod</string>
	<key>CFBundlePackageType</key>
	<string>APPL</string>
	<key>CFBundleShortVersionString</key>
	<string>$short</string>
	<key>CFBundleVersion</key>
	<string>$short</string>
	<key>LSApplicationCategoryType</key>
	<string>public.app-category.developer-tools</string>
	<key>LSMinimumSystemVersion</key>
	<string>12.0</string>
	<key>NSHighResolutionCapable</key>
	<true/>
	<key>NSSupportsAutomaticGraphicsSwitching</key>
	<true/>
</dict>
</plist>
PLIST

# Not notarized: an ad-hoc signature is the least macOS needs to run it.
codesign --force --deep --sign - "$app"
codesign --force --sign - "$out/quod"
