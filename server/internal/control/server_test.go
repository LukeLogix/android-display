package control

import (
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/luke/android-mac/server/internal/protocol"
)

func TestHandshake(t *testing.T) {
	srv, err := NewServer(0, 0.75, 4_000_000) // port 0 = random available port
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer srv.Stop()

	srv.SetTouchPort(9001)

	clientReady := make(chan ClientConn, 1)
	srv.OnClient(func(c ClientConn) {
		clientReady <- c
	})

	go srv.AcceptLoop()

	conn, err := net.DialTimeout("tcp", srv.Addr(), 2*time.Second)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	// Step 1: Send ClientHello
	hello := protocol.ClientHello{
		Device:       "Tab S6 Lite",
		Screen:       protocol.ScreenInfo{Width: 2304, Height: 1020, DPI: 224},
		Capabilities: []string{"touch", "pen"},
		Codecs:       []string{"h264"},
	}
	enc := json.NewEncoder(conn)
	if err := enc.Encode(hello); err != nil {
		t.Fatalf("failed to send hello: %v", err)
	}

	// Step 2: Read ServerHello
	var response protocol.ServerHello
	decoder := json.NewDecoder(conn)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err := decoder.Decode(&response); err != nil {
		t.Fatalf("failed to read response: %v", err)
	}

	if response.Codec != "h264" {
		t.Errorf("expected codec h264, got %s", response.Codec)
	}
	// Server applies its scale and rounds down to even dimensions (1020*0.75 = 765 → 764).
	if response.VirtualDisplay.Width != 1728 || response.VirtualDisplay.Height != 764 {
		t.Errorf("expected 1728x764, got %dx%d", response.VirtualDisplay.Width, response.VirtualDisplay.Height)
	}
	if response.Bitrate != 4_000_000 {
		t.Errorf("expected bitrate 4000000, got %d", response.Bitrate)
	}
	if response.StreamPort == 0 {
		t.Error("stream port should not be 0")
	}
	if response.TouchPort != 9001 {
		t.Errorf("expected touch port 9001, got %d", response.TouchPort)
	}

	// Step 3: Send ClientReady with the actual UDP port
	ready := protocol.ClientReady{UDPPort: 12345}
	if err := enc.Encode(ready); err != nil {
		t.Fatalf("failed to send client ready: %v", err)
	}

	// Verify the server received the UDP port
	select {
	case client := <-clientReady:
		if client.UDPPort != 12345 {
			t.Errorf("expected UDP port 12345, got %d", client.UDPPort)
		}
		if client.Width != 1728 || client.Height != 764 || client.Bitrate != 4_000_000 {
			t.Errorf("client conn has %dx%d @ %d, want 1728x764 @ 4000000", client.Width, client.Height, client.Bitrate)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for client ready callback")
	}
}

func TestHandshakeTimeout(t *testing.T) {
	srv, err := NewServer(0, 1, 8_000_000)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer srv.Stop()

	go srv.AcceptLoop()

	// Connect but never send anything — server should close the connection
	// due to the 10-second handshake timeout.
	conn, err := net.DialTimeout("tcp", srv.Addr(), 2*time.Second)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	// The server has a 10s handshake timeout. We wait a bit longer to verify
	// it closes the connection. Read should eventually fail.
	buf := make([]byte, 1)
	conn.SetReadDeadline(time.Now().Add(12 * time.Second))
	_, readErr := conn.Read(buf)
	if readErr == nil {
		t.Error("expected read to fail after handshake timeout, but it succeeded")
	}
}
