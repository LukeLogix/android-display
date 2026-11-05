# AGENTS.md

Guide for coding agents (and humans) working on android-display: an Android phone/tablet as an extended display for macOS, over USB (adb reverse) or WiFi.

## Layout

| Path | What |
|---|---|
| `server/` | Go + cgo/Objective-C server for the Mac. Creates a `CGVirtualDisplay` (private API), captures it, encodes H.264 with VideoToolbox, streams it, injects touch as mouse events. |
| `server/cmd/server/main.go` | Entry point, flags, pipeline wiring, display position persistence. |
| `server/internal/control` | TCP handshake (ClientHello → ServerHello → ClientReady). The server picks stream size and bitrate here. |
| `server/internal/display` | Virtual display creation (`display_bridge.m`), origin get/set. |
| `server/internal/input` | Touch → mouse injection (`injector.go`, `input_bridge.m`) and gesture recognition. |
| `server/internal/protocol` | JSON handshake messages. Must stay in sync with `client/.../protocol/Messages.kt`. |
| `client/` | Android app (Kotlin). Receives video (UDP for WiFi, TCP for USB), decodes with MediaCodec, sends normalised touch. |
| `mac-app/` | SwiftUI front-end (`AndroidDisplayApp.swift`) that bundles and supervises the server; `build.sh` builds it. |

## Build

Requirements: macOS 12.3+ (mac app: 13+), Xcode command line tools, Go 1.21+, `adb` (`brew install android-platform-tools`), JDK 17 or 21 and Android SDK 34 for the client.

```bash
# Server only
cd server && go build -o android-display-server ./cmd/server

# macOS app (builds the server too) → ~/Applications/AndroidDisplay.app
mac-app/build.sh

# Android client — Gradle 8.5 / AGP 8.2 need JDK 17 or 21 (not 25)
cd client
export JAVA_HOME=$(/usr/libexec/java_home -v 21)   # or the Homebrew openjdk@21 path
echo "sdk.dir=$ANDROID_HOME" > local.properties     # if the SDK is not auto-detected
./gradlew assembleDebug
adb install -r app/build/outputs/apk/debug/app-debug.apk
```

`swiftc` needs `-enable-bare-slash-regex` for the regex literals in the mac app (already in `build.sh`).

## Run

```bash
./server/android-display-server -scale 0.75 -bitrate 4000000
```

- `-scale` (0.25–1): streamed size as a fraction of the device's native screen. `-bitrate` in bps (default 8 Mbps). The Android app has no quality settings; it only reports its native size.
- `-exit-on-stdin-eof`: shut down when stdin closes. The mac app uses this so the server never outlives it.
- USB: the server sets up `adb reverse` **only at startup**. If the phone is plugged in later, restart the server.
- On the phone, pick USB or WiFi and tap Conectar.

Logs: the mac app writes the server log to `~/Library/Logs/AndroidDisplay.log` (truncated on each app launch). The app UI state is derived by parsing those log lines (`ServerController.handle(line:)`). If you change a server log message, update the parser.

## macOS permissions (common source of "it doesn't work")

- **Screen Recording** is needed to capture the virtual display. Without it the log shows `CGDisplayStreamCreate failed … (after 10 attempts)`.
- **Accessibility** is needed to inject touches. Without it the log shows `WARNING: Accessibility permission not granted!` and touches are silently dropped.
- Permissions belong to whatever launches the server: the terminal when run by hand, `AndroidDisplay.app` when run from the app.
- TCC binds permissions to the code signature. Ad-hoc signatures change on every build, so grants stop matching (`log show --predicate 'subsystem == "com.apple.TCC"'` shows `Failed to match existing code requirement`). `build.sh` signs with a keychain identity named `AndroidDisplay Local Signing` when present, which keeps grants across rebuilds. Create one with a self-signed code-signing certificate (EKU `codeSigning`) imported into the login keychain with `-T /usr/bin/codesign`.
- Reset stale grants: `tccutil reset Accessibility local.futit.androiddisplay` (same for `ScreenCapture`). macOS needs an app relaunch after granting Screen Recording.

## Gotchas

- The virtual display can move after it is created (macOS restoring the arrangement, user dragging it). Never cache its bounds; query `CGDisplayBounds` when needed.
- The display needs a fixed non-zero `serialNum`. With 0, macOS gives it a random UUID per connection and forgets its arrangement.
- The display position is saved in global coordinates to `~/Library/Application Support/AndroidDisplay/display-origin`. If other displays are rearranged, the saved spot may be stale. Moving it once in System Settings fixes it.
- Changing scale or bitrate requires a new connection: the client sizes its decoder from ServerHello.
- Port 9000 is fixed. "address already in use" means another server instance is still running.
- On the phone, set `adb shell settings put global stay_on_while_plugged_in 7` so the screen does not sleep. Some phones report charging over USB as AC, and plain `2` (USB only) has no effect on them.

## Tests

```bash
cd server && go test ./...
```

`internal/control` covers the handshake, including server-side scaling and even rounding. At the time of writing, `internal/stream` (`packet_test.go` type mismatch) and `internal/input` (`TestMoveWithinThresholdDoesNotDrag`) already fail upstream. Don't treat those as regressions from your change.

## Verifying end to end without touching the phone

With the phone on USB and USB debugging enabled:

```bash
adb shell monkey -p com.androidmac.client -c android.intent.category.LAUNCHER 1   # open app
adb exec-out screencap -p > /tmp/phone.png                                        # see its screen
adb shell input tap <x> <y>                                                       # tap USB, then Conectar
```

Then check the log for `pipeline active (video + touch)`. To check touch, tap the centre of the phone and read the cursor position (e.g. a 3-line Swift script printing `CGEvent(source: nil)!.location`). It should land inside the virtual display's `CGDisplayBounds`.

## Conventions

- Keep the Go and Kotlin protocol structs in sync. The server is the source of truth for stream parameters.
- Run `gofmt` on Go code. Don't add Go dependencies without a reason.
- Comments and UI strings are partly in Traditional Chinese. Keep user-facing text in `strings.xml`.
