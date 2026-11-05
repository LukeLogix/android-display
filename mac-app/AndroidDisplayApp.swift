// Android Display — Mac front-end for android-display-server.
// Runs the bundled server as a child process with the chosen resolution scale and
// bitrate, shows connection status parsed from its log, and stops it on quit.

import AppKit
import ApplicationServices
import CoreGraphics
import SwiftUI

private let scaleOptions: [Double] = [1.0, 0.9, 0.75, 0.6, 0.5]
private let bitrateOptions: [Int] = [12_000_000, 8_000_000, 6_000_000, 4_000_000, 2_000_000, 1_000_000]

enum ServerState {
    case stopped, waiting, connected
}

struct ServerConfig: Equatable {
    var scale: Double
    var bitrate: Int
}

@MainActor
final class ServerController: ObservableObject {
    static let shared = ServerController()

    @Published private(set) var state: ServerState = .stopped
    @Published private(set) var running: ServerConfig?
    @Published private(set) var device: String?
    @Published private(set) var nativeSize: (width: Int, height: Int)?
    @Published private(set) var streamSize: String?
    @Published private(set) var noAdbDevice = false
    @Published private(set) var error: String?

    let logURL = FileManager.default.homeDirectoryForCurrentUser
        .appendingPathComponent("Library/Logs/AndroidDisplay.log")

    private var process: Process?
    private var stdinPipe: Pipe?
    private var outputPipe: Pipe?
    private var lineBuffer = Data()

    private init() {
        FileManager.default.createFile(atPath: logURL.path, contents: nil)
    }

    func start(_ config: ServerConfig) {
        guard process == nil else { return }
        guard let binary = Bundle.main.url(forResource: "android-display-server", withExtension: nil) else {
            error = "No se encontró android-display-server dentro de la app."
            return
        }

        let proc = Process()
        proc.executableURL = binary
        proc.arguments = [
            "-scale", String(config.scale),
            "-bitrate", String(config.bitrate),
            "-exit-on-stdin-eof",
        ]
        var env = ProcessInfo.processInfo.environment
        env["PATH"] = "/opt/homebrew/bin:/usr/local/bin:" + (env["PATH"] ?? "/usr/bin:/bin")
        proc.environment = env

        // The server exits when this pipe closes, so it never outlives the app.
        let stdin = Pipe()
        proc.standardInput = stdin
        let output = Pipe()
        proc.standardOutput = output
        proc.standardError = output

        let logURL = self.logURL
        output.fileHandleForReading.readabilityHandler = { handle in
            let data = handle.availableData
            guard !data.isEmpty else {
                handle.readabilityHandler = nil // EOF: the server exited
                return
            }
            if let log = try? FileHandle(forWritingTo: logURL) {
                log.seekToEndOfFile()
                log.write(data)
                try? log.close()
            }
            Task { @MainActor in ServerController.shared.consume(data) }
        }

        proc.terminationHandler = { p in
            Task { @MainActor in ServerController.shared.didTerminate(p) }
        }

        do {
            try proc.run()
        } catch {
            self.error = "No se pudo iniciar el servidor: \(error.localizedDescription)"
            return
        }
        process = proc
        stdinPipe = stdin
        outputPipe = output
        running = config
        error = nil
        state = .waiting
    }

    /// Stops the server and waits (up to ~3 s) for it to clean up its adb forwards.
    func stop() {
        guard let proc = process else { return }
        // Detach first: late exit/log events from this process must not touch the next one.
        proc.terminationHandler = nil
        proc.terminate()
        let deadline = Date().addingTimeInterval(3)
        while proc.isRunning && Date() < deadline {
            usleep(50_000)
        }
        if proc.isRunning {
            kill(proc.processIdentifier, SIGKILL)
            proc.waitUntilExit()
        }
        outputPipe?.fileHandleForReading.readabilityHandler = nil
        try? stdinPipe?.fileHandleForWriting.close()
        reset()
    }

    func restart(_ config: ServerConfig) {
        stop()
        start(config)
    }

    private func reset() {
        process = nil
        stdinPipe = nil
        outputPipe = nil
        running = nil
        state = .stopped
        device = nil
        streamSize = nil
        lineBuffer.removeAll()
    }

    private func didTerminate(_ proc: Process) {
        // Only an unexpected exit of the current server gets here; stop() detaches its handler.
        guard proc === process else { return }
        let status = proc.terminationStatus
        if error == nil {
            error = "El servidor se cerró (código \(status)). Revisá el log."
        }
        reset()
    }

    private func consume(_ data: Data) {
        lineBuffer.append(data)
        while let newline = lineBuffer.firstIndex(of: UInt8(ascii: "\n")) {
            let lineData = lineBuffer[lineBuffer.startIndex..<newline]
            lineBuffer.removeSubrange(lineBuffer.startIndex...newline)
            handle(line: String(decoding: lineData, as: UTF8.self))
        }
    }

    private func handle(line: String) {
        if line.contains("control server on port") {
            state = .waiting
        } else if let m = line.firstMatch(of: /client connected: (.+) \((\d+)x(\d+)\)/) {
            device = String(m.1)
            nativeSize = (Int(m.2) ?? 0, Int(m.3) ?? 0)
        } else if let m = line.firstMatch(of: /stream config: (\d+)x(\d+)/) {
            streamSize = "\(m.1)×\(m.2)"
        } else if line.contains("pipeline active") {
            state = .connected
            error = nil
        } else if line.contains("client disconnected") || line.contains("pipeline stopping") {
            state = .waiting
            streamSize = nil
        } else if line.contains("ADB available but no device connected") {
            noAdbDevice = true
        } else if line.contains("ADB reverse forwarding enabled") {
            noAdbDevice = false
        } else if line.contains("address already in use") {
            error = "El puerto 9000 está ocupado: hay otro servidor corriendo."
        } else if line.contains("capture error") {
            error = "No se pudo capturar la pantalla virtual. Dale permiso de Grabación de pantalla, cerrá y volvé a abrir la app."
        } else if line.contains("virtual display error") || line.contains("encoder error") {
            error = "Falló el pipeline de video. Revisá el log."
        }
    }
}

final class AppDelegate: NSObject, NSApplicationDelegate {
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { true }

    func applicationWillTerminate(_ notification: Notification) {
        MainActor.assumeIsolated { ServerController.shared.stop() }
    }
}

@main
struct AndroidDisplayApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) private var delegate

    var body: some Scene {
        Window("Android Display", id: "main") {
            ContentView()
        }
        .windowResizability(.contentSize)
    }
}

struct ContentView: View {
    @ObservedObject private var server = ServerController.shared
    @AppStorage("scale") private var scale = 0.75
    @AppStorage("bitrate") private var bitrate = 4_000_000
    @State private var accessibilityOK = AXIsProcessTrusted()
    @State private var screenCaptureOK = CGPreflightScreenCaptureAccess()

    private let permissionTimer = Timer.publish(every: 2, on: .main, in: .common).autoconnect()

    private var selected: ServerConfig { ServerConfig(scale: scale, bitrate: bitrate) }
    private var hasPendingChanges: Bool { server.running != nil && server.running != selected }

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            status

            Divider()

            Form {
                Picker("Resolución", selection: $scale) {
                    ForEach(scaleOptions, id: \.self) { Text(scaleLabel($0)).tag($0) }
                }
                Picker("Calidad", selection: $bitrate) {
                    ForEach(bitrateOptions, id: \.self) { Text(bitrateLabel($0)).tag($0) }
                }
            }

            if hasPendingChanges {
                Text("Los cambios se aplican al reiniciar el servidor. El celular se desconecta: tocá Conectar de nuevo.")
                    .font(.callout)
                    .foregroundStyle(.orange)
                    .fixedSize(horizontal: false, vertical: true)
            }

            HStack {
                if server.running == nil {
                    Button("Iniciar") { server.start(selected) }
                        .keyboardShortcut(.defaultAction)
                } else {
                    Button("Aplicar y reiniciar") { server.restart(selected) }
                        .keyboardShortcut(.defaultAction)
                    Button("Detener") { server.stop() }
                }
                Spacer()
                Button("Ver log") { NSWorkspace.shared.open(server.logURL) }
            }

            if !accessibilityOK || !screenCaptureOK {
                Divider()
                permissions
            }
        }
        .padding(20)
        .frame(width: 440)
        .onAppear { server.start(selected) }
        .onReceive(permissionTimer) { _ in
            accessibilityOK = AXIsProcessTrusted()
            screenCaptureOK = CGPreflightScreenCaptureAccess()
        }
    }

    private var status: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 8) {
                Circle().fill(statusColor).frame(width: 10, height: 10)
                Text(statusTitle).font(.headline)
            }
            if let detail = statusDetail {
                Text(detail).font(.callout).foregroundStyle(.secondary)
            }
            if let error = server.error {
                Text(error).font(.callout).foregroundStyle(.red)
            }
            if server.noAdbDevice {
                Text("No había celular por USB al iniciar. Conectalo y tocá Aplicar y reiniciar para habilitar USB.")
                    .font(.callout)
                    .foregroundStyle(.orange)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
    }

    private var permissions: some View {
        VStack(alignment: .leading, spacing: 8) {
            if !accessibilityOK {
                HStack {
                    Text("Falta permiso de Accesibilidad (toques del celular).").font(.callout)
                    Spacer()
                    Button("Abrir") {
                        openSettings("x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility")
                    }
                }
            }
            if !screenCaptureOK {
                HStack {
                    Text("Falta permiso de Grabación de pantalla.").font(.callout)
                    Spacer()
                    Button("Pedir") { CGRequestScreenCaptureAccess() }
                }
            }
        }
    }

    private var statusColor: Color {
        switch server.state {
        case .stopped: return .gray
        case .waiting: return .orange
        case .connected: return .green
        }
    }

    private var statusTitle: String {
        switch server.state {
        case .stopped: return "Servidor detenido"
        case .waiting: return "Esperando al celular…"
        case .connected: return "Conectado" + (server.device.map { ": \($0)" } ?? "")
        }
    }

    private var statusDetail: String? {
        guard let running = server.running else { return nil }
        var parts: [String] = []
        if server.state == .connected, let size = server.streamSize {
            parts.append(size)
        }
        parts.append("\(Int(running.scale * 100)) %")
        parts.append(bitrateLabel(running.bitrate))
        return parts.joined(separator: " · ")
    }

    private func scaleLabel(_ s: Double) -> String {
        let pct = "\(Int(s * 100)) %"
        guard let native = server.nativeSize else { return pct }
        let w = Int(Double(native.width) * s) & ~1
        let h = Int(Double(native.height) * s) & ~1
        return "\(pct)  (\(w)×\(h))"
    }

    private func bitrateLabel(_ b: Int) -> String { "\(b / 1_000_000) Mbps" }

    private func openSettings(_ url: String) {
        if let url = URL(string: url) { NSWorkspace.shared.open(url) }
    }
}
