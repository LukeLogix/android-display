package control

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/luke/android-mac/server/internal/protocol"
)

type ClientConn struct {
	Conn    net.Conn
	Hello   protocol.ClientHello
	UDPPort int // actual UDP port reported by the client via ClientReady (0 for USB)
	// Negotiated stream parameters (also sent to the client in ServerHello).
	Width   int
	Height  int
	Bitrate int
}

type Server struct {
	listener   net.Listener
	clients    []ClientConn
	mu         sync.Mutex
	streamPort int
	touchPort  int
	videoPort  int // TCP video port for USB mode
	scale      float64
	bitrate    int
	onClient   func(ClientConn)
	done       chan struct{}
}

func NewServer(port int, scale float64, bitrate int) (*Server, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, err
	}
	return &Server{
		listener:   ln,
		streamPort: 5001,
		scale:      scale,
		bitrate:    bitrate,
		done:       make(chan struct{}),
	}, nil
}

// scaleEven scales a dimension and rounds down to an even number (H.264 requirement).
func scaleEven(v int, scale float64) int {
	return int(float64(v)*scale) &^ 1
}

func (s *Server) Addr() string {
	return s.listener.Addr().String()
}

func (s *Server) Port() int {
	return s.listener.Addr().(*net.TCPAddr).Port
}

func (s *Server) SetStreamPort(port int) {
	s.streamPort = port
}

func (s *Server) SetTouchPort(port int) {
	s.touchPort = port
}

func (s *Server) SetVideoPort(port int) {
	s.videoPort = port
}

func (s *Server) OnClient(fn func(ClientConn)) {
	s.onClient = fn
}

func (s *Server) AcceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
				log.Printf("accept error: %v", err)
				continue
			}
		}
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	// I6: Enforce a 10-second deadline for the entire handshake phase.
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))

	decoder := json.NewDecoder(conn)

	// Step 1: Read ClientHello
	var hello protocol.ClientHello
	if err := decoder.Decode(&hello); err != nil {
		log.Printf("handshake read error: %v", err)
		conn.Close()
		return
	}

	connType := "WiFi"
	if hello.IsUSB() {
		connType = "USB"
	}
	log.Printf("client connected: %s (%dx%d) [%s]", hello.Device, hello.Screen.Width, hello.Screen.Height, connType)

	codec := "h264"
	for _, c := range hello.Codecs {
		if c == "h264" {
			codec = "h264"
			break
		}
	}

	// Step 2: Send ServerHello with the server-chosen size and bitrate
	width := scaleEven(hello.Screen.Width, s.scale)
	height := scaleEven(hello.Screen.Height, s.scale)
	log.Printf("stream config: %dx%d @ %d bps (scale %.2f)", width, height, s.bitrate, s.scale)
	response := protocol.ServerHello{
		VirtualDisplay: protocol.DisplayInfo{
			Width:  width,
			Height: height,
		},
		Codec:      codec,
		Bitrate:    s.bitrate,
		FPS:        60,
		StreamPort: s.streamPort,
		TouchPort:  s.touchPort,
		VideoPort:  s.videoPort,
	}

	enc := json.NewEncoder(conn)
	if err := enc.Encode(response); err != nil {
		log.Printf("handshake write error: %v", err)
		conn.Close()
		return
	}

	// Step 3: Read ClientReady to get the actual UDP port
	var ready protocol.ClientReady
	if err := decoder.Decode(&ready); err != nil {
		log.Printf("client ready read error: %v", err)
		conn.Close()
		return
	}
	if hello.IsUSB() {
		log.Println("client ready: USB mode (TCP video)")
	} else {
		log.Printf("client ready: UDP port %d", ready.UDPPort)
	}

	// Clear the read deadline after successful handshake.
	conn.SetReadDeadline(time.Time{})

	client := ClientConn{Conn: conn, Hello: hello, UDPPort: ready.UDPPort, Width: width, Height: height, Bitrate: s.bitrate}
	s.mu.Lock()
	s.clients = append(s.clients, client)
	s.mu.Unlock()

	if s.onClient != nil {
		s.onClient(client)
	}
}

func (s *Server) Stop() {
	close(s.done)
	s.listener.Close()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.clients {
		c.Conn.Close()
	}
}
