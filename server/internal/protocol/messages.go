package protocol

// ClientHello is sent by Android to Mac during handshake.
// Screen carries the device's native resolution; the server decides the
// streamed size and bitrate and reports them back in ServerHello.
type ClientHello struct {
	Device         string     `json:"device"`
	Screen         ScreenInfo `json:"screen"`
	Capabilities   []string   `json:"capabilities"`
	Codecs         []string   `json:"codecs"`
	ConnectionType string     `json:"connectionType,omitempty"` // "wifi" (default) or "usb"
}

// IsUSB returns true if the client is connected via USB/ADB.
func (h *ClientHello) IsUSB() bool {
	return h.ConnectionType == "usb"
}

type ScreenInfo struct {
	Width  int `json:"width"`
	Height int `json:"height"`
	DPI    int `json:"dpi"`
}

// ServerHello is sent by Mac to Android in response
type ServerHello struct {
	VirtualDisplay DisplayInfo `json:"virtualDisplay"`
	Codec          string      `json:"codec"`
	Bitrate        int         `json:"bitrate"`
	FPS            int         `json:"fps"`
	StreamPort     int         `json:"streamPort"`
	TouchPort      int         `json:"touchPort"`
	VideoPort      int         `json:"videoPort,omitempty"` // TCP video port for USB mode
}

type DisplayInfo struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// ClientReady is sent by Android to Mac after binding the UDP socket.
// For USB mode, UDPPort is 0 (video uses TCP instead).
type ClientReady struct {
	UDPPort int `json:"udpPort"`
}

// Heartbeat is sent periodically on the control channel
type Heartbeat struct {
	Timestamp int64 `json:"timestamp"`
}
