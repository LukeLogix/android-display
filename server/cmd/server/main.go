package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/luke/android-mac/server/internal/adb"
	"github.com/luke/android-mac/server/internal/capture"
	"github.com/luke/android-mac/server/internal/control"
	"github.com/luke/android-mac/server/internal/discovery"
	"github.com/luke/android-mac/server/internal/display"
	"github.com/luke/android-mac/server/internal/encoder"
	"github.com/luke/android-mac/server/internal/input"
	"github.com/luke/android-mac/server/internal/stream"
	"github.com/luke/android-mac/server/internal/touch"
)

const (
	controlPort    = 9000
	defaultFPS     = 60
	defaultBitrate = 8_000_000
)

// adbManager is set up at startup if ADB is available.
var adbManager *adb.Manager

// tcpVideoServer is always started for USB mode support.
var tcpVideoServer *stream.TCPVideoServer

func main() {
	scale := flag.Float64("scale", 1.0, "streamed resolution as a fraction of the device's native screen (0.25-1)")
	bitrate := flag.Int("bitrate", defaultBitrate, "video bitrate in bits per second")
	exitOnStdinEOF := flag.Bool("exit-on-stdin-eof", false, "shut down when stdin closes (set by the Mac app so the server dies with it)")
	flag.Parse()
	if *scale < 0.25 || *scale > 1 {
		log.Fatalf("-scale must be between 0.25 and 1, got %v", *scale)
	}
	if *bitrate <= 0 {
		log.Fatalf("-bitrate must be positive, got %d", *bitrate)
	}

	log.Println("android-mac server starting...")

	// 1. Start touch TCP server (auto-assign port)
	touchServer, err := touch.NewServer(0)
	if err != nil {
		log.Fatalf("touch server failed: %v", err)
	}
	defer touchServer.Stop()
	go touchServer.AcceptLoop()
	log.Printf("touch server on port %d", touchServer.Port())

	// 2. Start TCP video server for USB mode (auto-assign port)
	tcpVideoServer, err = stream.NewTCPVideoServer(0)
	if err != nil {
		log.Fatalf("TCP video server failed: %v", err)
	}
	defer tcpVideoServer.Close()
	log.Printf("TCP video server on port %d", tcpVideoServer.Port())

	// 3. Set up ADB reverse forwarding if device is connected
	adbManager, err = adb.NewManager()
	if err != nil {
		log.Printf("ADB not available: %v (USB connections disabled)", err)
	} else if adbManager.HasDevice() {
		// Clean up any stale reverse forwarding from previous runs
		adbManager.RemoveAllReverse()

		// Set up reverse forwarding for all ports
		if err := adbManager.SetupReverse(controlPort); err != nil {
			log.Printf("ADB reverse control port failed: %v", err)
		}
		if err := adbManager.SetupReverse(touchServer.Port()); err != nil {
			log.Printf("ADB reverse touch port failed: %v", err)
		}
		if err := adbManager.SetupReverse(tcpVideoServer.Port()); err != nil {
			log.Printf("ADB reverse video port failed: %v", err)
		}
		log.Println("ADB reverse forwarding enabled (USB connections ready)")
	} else {
		log.Println("ADB available but no device connected")
	}

	// 4. Start mDNS advertisement
	hostname, _ := os.Hostname()
	mdns, err := discovery.NewService(hostname, controlPort)
	if err != nil {
		log.Fatalf("mDNS failed: %v", err)
	}
	defer mdns.Stop()
	log.Printf("mDNS advertising on port %d", controlPort)

	// 5. Start TCP control server
	ctrlServer, err := control.NewServer(controlPort, *scale, *bitrate)
	if err != nil {
		log.Fatalf("control server failed: %v", err)
	}
	defer ctrlServer.Stop()
	ctrlServer.SetTouchPort(touchServer.Port())
	ctrlServer.SetVideoPort(tcpVideoServer.Port())

	// 6. On client connect → start the capture-encode-stream pipeline
	ctrlServer.OnClient(func(client control.ClientConn) {
		w, h, bitrate := client.Width, client.Height, client.Bitrate

		ctx, cancel := context.WithCancel(context.Background())

		// Monitor the TCP control connection
		go func() {
			buf := make([]byte, 1)
			for {
				_, err := client.Conn.Read(buf)
				if err != nil {
					if err != io.EOF {
						log.Printf("control connection read error: %v", err)
					}
					log.Println("client disconnected, cancelling pipeline")
					cancel()
					return
				}
			}
		}()

		// Determine video transport based on connection type
		if client.Hello.IsUSB() {
			go startPipelineUSB(ctx, cancel, w, h, bitrate, touchServer)
		} else {
			remoteAddr := client.Conn.RemoteAddr().String()
			host, _, _ := net.SplitHostPort(remoteAddr)
			targetAddr := fmt.Sprintf("%s:%d", host, client.UDPPort)
			go startPipelineWiFi(ctx, cancel, w, h, bitrate, targetAddr, touchServer)
		}
	})

	go ctrlServer.AcceptLoop()
	log.Printf("control server on port %d — waiting for connections...", controlPort)

	// 7. Wait for SIGINT/SIGTERM
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	if *exitOnStdinEOF {
		go func() {
			io.Copy(io.Discard, os.Stdin)
			sig <- syscall.SIGTERM
		}()
	}
	<-sig
	log.Println("shutting down...")

	// Clean up ADB reverse forwarding
	if adbManager != nil {
		adbManager.RemoveAllReverse()
	}
}

// startPipelineWiFi starts the capture-encode-stream pipeline using UDP transport.
func startPipelineWiFi(ctx context.Context, cancel context.CancelFunc, width, height, bitrate int, targetAddr string, touchServer *touch.Server) {
	defer cancel()
	log.Printf("starting WiFi pipeline: %dx%d @ %d bps → %s", width, height, bitrate, targetAddr)

	// UDP streamer
	udpStreamer, err := stream.NewUDPStreamer(targetAddr)
	if err != nil {
		log.Printf("UDP streamer error: %v", err)
		return
	}
	defer udpStreamer.Close()

	startPipelineCommon(ctx, cancel, width, height, bitrate, udpStreamer, touchServer)
}

// startPipelineUSB starts the capture-encode-stream pipeline using TCP transport.
func startPipelineUSB(ctx context.Context, cancel context.CancelFunc, width, height, bitrate int, touchServer *touch.Server) {
	defer cancel()
	log.Printf("starting USB pipeline: %dx%d @ %d bps (TCP video)", width, height, bitrate)

	// Wait for client to connect to TCP video port
	if err := tcpVideoServer.AcceptOne(); err != nil {
		log.Printf("TCP video accept error: %v", err)
		return
	}

	startPipelineCommon(ctx, cancel, width, height, bitrate, tcpVideoServer, touchServer)
}

// startPipelineCommon contains the shared pipeline logic for both WiFi and USB modes.
func startPipelineCommon(ctx context.Context, cancel context.CancelFunc, width, height, bitrate int, videoStreamer stream.Streamer, touchServer *touch.Server) {
	// Virtual display
	vd, err := display.New(display.Config{
		Width:  width,
		Height: height,
		PPI:    224,
	})
	if err != nil {
		log.Printf("virtual display error: %v", err)
		return
	}
	defer vd.Close()
	log.Printf("virtual display created: 0x%x (%dx%d)", vd.DisplayID(), width, height)

	// macOS places a new virtual display at a default spot; put it back where the
	// user last arranged it. The position is saved whenever it changes (polled,
	// since a SIGTERM restart exits without running this function's defers).
	if x, y, ok := loadDisplayOrigin(); ok {
		if err := vd.SetOrigin(x, y); err != nil {
			log.Printf("restore display position: %v", err)
		} else {
			log.Printf("display position restored: (%d,%d)", x, y)
		}
	}
	stopTracking := make(chan struct{})
	trackingDone := make(chan struct{})
	go trackDisplayOrigin(vd, stopTracking, trackingDone)
	// Registered after vd.Close, so it runs first: tracking stops before the display is destroyed.
	defer func() {
		close(stopTracking)
		<-trackingDone
	}()

	// Input injector + gesture recognizer for this display
	injector := input.NewInjector(vd.DisplayID())
	gesture := input.NewGestureRecognizer(func(me input.MouseEvent) {
		switch me.Action {
		case input.ActionMouseMove:
			injector.MouseMove(me.X, me.Y)
		case input.ActionLeftDown:
			injector.LeftMouseDown(me.X, me.Y)
		case input.ActionLeftUp:
			injector.LeftMouseUp(me.X, me.Y)
		case input.ActionLeftDragged:
			injector.LeftMouseDragged(me.X, me.Y)
		case input.ActionRightDown:
			injector.RightMouseDown(me.X, me.Y)
		case input.ActionRightUp:
			injector.RightMouseUp(me.X, me.Y)
		case input.ActionScroll:
			injector.ScrollWheel(me.ScrollX, me.ScrollY)
		}
	})
	defer gesture.Close()

	// Wire touch events → pen events bypass gesture recognizer
	touchServer.OnEvent(func(e touch.Event) {
		if e.Type == touch.TouchTypePen {
			switch e.Action {
			case touch.TouchActionDown:
				injector.TabletProximityEnter()
				injector.TabletDown(e.X, e.Y, e.Pressure, e.TiltX, e.TiltY)
			case touch.TouchActionMove:
				injector.TabletDragged(e.X, e.Y, e.Pressure, e.TiltX, e.TiltY)
			case touch.TouchActionUp:
				injector.TabletUp(e.X, e.Y, e.Pressure, e.TiltX, e.TiltY)
				injector.TabletProximityLeave()
			}
		} else {
			gesture.HandleEvent(e)
		}
	})
	defer touchServer.OnEvent(nil)
	log.Println("touch input enabled")

	// H.264 encoder
	enc, err := encoder.New(encoder.Config{
		Width:   width,
		Height:  height,
		FPS:     defaultFPS,
		Bitrate: bitrate,
	}, func(nal encoder.NALUnit) {
		var ft byte
		switch {
		case nal.NALType == 7:
			ft = stream.FrameTypeSPS
		case nal.NALType == 8:
			ft = stream.FrameTypePPS
		case nal.IsKeyframe:
			ft = stream.FrameTypeIDR
		default:
			ft = stream.FrameTypeP
		}
		if err := videoStreamer.SendFrame(nal.Data, ft); err != nil {
			log.Printf("stream send error: %v", err)
		}
	})
	if err != nil {
		log.Printf("encoder error: %v", err)
		return
	}
	defer enc.Close()

	// Screen capture
	cap, err := capture.Start(vd.DisplayID(), defaultFPS, func(frame capture.Frame) {
		defer capture.ReleasePixelBuffer(frame.PixelBuffer)
		if err := enc.Encode(frame.PixelBuffer, frame.Timestamp); err != nil {
			log.Printf("encode error: %v", err)
		}
	})
	if err != nil {
		log.Printf("capture error: %v", err)
		return
	}
	defer cap.Stop()

	log.Println("pipeline active (video + touch)")
	<-ctx.Done()
	log.Println("pipeline stopping...")
}

// trackDisplayOrigin saves the display's position whenever the user moves it,
// and once more when stop is closed.
func trackDisplayOrigin(vd *display.VirtualDisplay, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	lastX, lastY := vd.Origin()
	save := func() {
		x, y := vd.Origin()
		if x == lastX && y == lastY {
			return
		}
		if err := saveDisplayOrigin(x, y); err != nil {
			log.Printf("save display position: %v", err)
			return
		}
		lastX, lastY = x, y
		log.Printf("display position saved: (%d,%d)", x, y)
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			save()
			return
		case <-ticker.C:
			save()
		}
	}
}

// displayOriginPath is where the virtual display's last position is stored.
func displayOriginPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "AndroidDisplay", "display-origin"), nil
}

func loadDisplayOrigin() (x, y int, ok bool) {
	path, err := displayOriginPath()
	if err != nil {
		return 0, 0, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, false
	}
	if _, err := fmt.Sscanf(string(data), "%d %d", &x, &y); err != nil {
		return 0, 0, false
	}
	return x, y, true
}

func saveDisplayOrigin(x, y int) error {
	path, err := displayOriginPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(fmt.Sprintf("%d %d\n", x, y)), 0o644)
}
