# android-display

Turn your Android tablet into a wireless extended display for macOS, with touch and S Pen pressure support.

## Features

- **Wireless Extended Display** — Creates a real macOS virtual display on your Android tablet over WiFi
- **USB Mode** — Also supports USB connection via ADB reverse forwarding for lower latency
- **Touch Input** — Tap, drag, scroll, long-press right-click, double-tap on the extended display
- **S Pen Pressure** — Samsung S Pen pressure and tilt data mapped to macOS tablet events for drawing apps
- **Auto Discovery** — mDNS/Bonjour zero-config, Android app finds your Mac automatically
- **Hardware Encoding** — H.264 via VideoToolbox (Mac) and MediaCodec (Android), low latency
- **Adjustable Quality** — 8M / 4M / 2M / 1M bitrate selector

## Architecture

```
Mac (Go + CGo Server)                    Android (Kotlin Client)
┌──────────────────────┐    WiFi/UDP    ┌──────────────────────┐
│ CGVirtualDisplay     │───────────────▶│ VideoReceiver        │
│ (private API)        │                │                      │
│                      │                │ MediaCodec H.264     │
│ CGDisplayStream      │                │ Decode               │
│ (screen capture)     │                │                      │
│                      │                │ SurfaceView          │
│ VideoToolbox H.264   │                │ (fullscreen display) │
│ (hw encode)          │                │                      │
│                      │◀───────────────│ TouchCollector       │
│ CGEvent / CGTablet   │    TCP         │ (finger + S Pen)     │
│ (input injection)    │                │                      │
│                      │                │ TouchSender (TCP)    │
│ Bonjour mDNS         │   mDNS        │ NSD Discovery        │
│                      │◀─────────────▶│                      │
└──────────────────────┘                └──────────────────────┘
```

## Requirements

### Mac (Server)
- macOS 12.3+ (Monterey or later)
- Xcode Command Line Tools (`xcode-select --install`)
- Go 1.21+

### Android (Client)
- Android 10+ (API 29+)
- Android Studio (to build the APK)
- Recommended: Samsung tablet with S Pen for pressure support

## Build

### Mac Server

```bash
cd server
go build -o android-display-server ./cmd/server
```

### Android Client

Open the `client/` directory in Android Studio and build the APK, or:

```bash
cd client
./gradlew assembleDebug
```

Install the APK on your Android device:

```bash
adb install client/app/build/outputs/apk/debug/app-debug.apk
```

### Mac App (optional)

`mac-app/` is a SwiftUI front-end that bundles the server, picks the streamed resolution (as a fraction of the device screen) and bitrate, shows connection status, and stops the server on quit:

```bash
mac-app/build.sh   # installs ~/Applications/AndroidDisplay.app
```

Grant the app Accessibility and Screen Recording permission. `build.sh` signs with a local `AndroidDisplay Local Signing` certificate when it exists in the keychain, so the grants survive rebuilds; otherwise it signs ad hoc and macOS asks again after each build. The server log is at `~/Library/Logs/AndroidDisplay.log`.

The virtual display keeps a stable identity, and its last position (System Settings → Displays → Arrange) is saved to `~/Library/Application Support/AndroidDisplay/display-origin` and restored on the next connection.

## Usage

1. **Start the Mac server** (or open `AndroidDisplay.app`):
   ```bash
   ./server/android-display-server -scale 0.75 -bitrate 4000000
   ```
   `-scale` (0.25–1, default 1) sets the streamed resolution relative to the device's native screen; `-bitrate` defaults to 8 Mbps. Both are chosen on the Mac — the Android app only reports its native resolution.

2. **Open the Android app** on your tablet — it will auto-discover the Mac via mDNS

3. **Tap to connect** — the tablet becomes an extended display

4. **Drag windows** from your Mac to the extended display

### USB Mode

For lower latency, connect your Android device via USB:

1. Enable USB debugging on your Android device
2. Connect via USB cable
3. Start the server (it will automatically set up ADB reverse forwarding)
4. Open the Android app and connect

## Protocol

| Channel       | Protocol | Direction     | Purpose                    |
|---------------|----------|---------------|----------------------------|
| Video Stream  | UDP      | Mac → Android | Low-latency H.264 video    |
| Touch Events  | TCP      | Android → Mac | Reliable touch/pen input   |
| Control       | TCP      | Bidirectional | Handshake, config          |
| Discovery     | mDNS     | LAN broadcast | Auto-discover Mac server   |

## Known Limitations

- Uses `CGVirtualDisplay` private API — may break on future macOS updates
- WiFi 5GHz recommended for best streaming quality
- macOS may require Screen Recording permission for screen capture

## License

[Apache License 2.0](LICENSE)
