#!/bin/bash
# Builds AndroidDisplay.app (SwiftUI front-end + bundled Go server) into ~/Applications.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
APP="${APP:-$HOME/Applications/AndroidDisplay.app}"
BUILD="$ROOT/mac-app/build"

mkdir -p "$BUILD"
(cd "$ROOT/server" && go build -o "$BUILD/android-display-server" ./cmd/server)
swiftc -parse-as-library -O -enable-bare-slash-regex -target arm64-apple-macos13.0 \
    -o "$BUILD/AndroidDisplay" "$ROOT/mac-app/AndroidDisplayApp.swift"

rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
cp "$BUILD/AndroidDisplay" "$APP/Contents/MacOS/AndroidDisplay"
cp "$BUILD/android-display-server" "$APP/Contents/Resources/android-display-server"
cat > "$APP/Contents/Info.plist" <<'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleName</key><string>AndroidDisplay</string>
<key>CFBundleDisplayName</key><string>Android Display</string>
<key>CFBundleIdentifier</key><string>local.futit.androiddisplay</string>
<key>CFBundleExecutable</key><string>AndroidDisplay</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>CFBundleVersion</key><string>2</string>
<key>CFBundleShortVersionString</key><string>2.0</string>
<key>LSMinimumSystemVersion</key><string>13.0</string>
<key>NSPrincipalClass</key><string>NSApplication</string>
<key>NSHighResolutionCapable</key><true/>
</dict></plist>
EOF

# Sign with a stable local certificate when available so macOS keeps the
# Accessibility / Screen Recording grants across rebuilds (ad-hoc cdhash changes every build).
SIGN_ID="${SIGN_ID:-AndroidDisplay Local Signing}"
if ! security find-identity -p codesigning | grep -q "\"$SIGN_ID\""; then
    SIGN_ID="-"
fi
codesign --force --deep --sign "$SIGN_ID" "$APP"
echo "Built $APP"
