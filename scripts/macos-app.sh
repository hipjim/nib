#!/bin/sh
# Builds a universal (Apple Silicon + Intel) nib.app and nib binary.
# Usage: scripts/macos-app.sh [version] [outdir]
set -eu
version=${1:-dev}
out=${2:-dist}
ldflags="-s -w -X main.version=$version"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$out"

for arch in arm64 amd64; do
	GOOS=darwin GOARCH=$arch CGO_ENABLED=1 go build -trimpath -ldflags "$ldflags" -o "$work/nib-$arch" .
done
lipo -create -output "$out/nib" "$work/nib-arm64" "$work/nib-amd64"

app="$out/nib.app"
rm -rf "$app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
cp "$out/nib" "$app/Contents/MacOS/nib"

iconset="$work/nib.iconset"
mkdir "$iconset"
for s in 16 32 128 256 512; do
	sips -z $s $s appicon.png --out "$iconset/icon_${s}x${s}.png" >/dev/null
	sips -z $((s * 2)) $((s * 2)) appicon.png --out "$iconset/icon_${s}x${s}@2x.png" >/dev/null
done
iconutil -c icns -o "$app/Contents/Resources/nib.icns" "$iconset"

short=${version#v}
case $short in [0-9]*) ;; *) short=0.0.0 ;; esac
cat >"$app/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleName</key>
	<string>nib</string>
	<key>CFBundleExecutable</key>
	<string>nib</string>
	<key>CFBundleIdentifier</key>
	<string>io.github.hipjim.nib</string>
	<key>CFBundleIconFile</key>
	<string>nib</string>
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
codesign --force --sign - "$out/nib"
