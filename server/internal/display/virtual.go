package display

/*
#cgo CFLAGS: -x objective-c -fobjc-arc -I../../bridge
#cgo LDFLAGS: -framework CoreGraphics -framework Foundation
#include "../../bridge/display_bridge.h"
#include <CoreGraphics/CoreGraphics.h>

static CGError SetDisplayOrigin(CGDirectDisplayID id, int x, int y) {
	CGDisplayConfigRef cfg;
	CGError err = CGBeginDisplayConfiguration(&cfg);
	if (err != kCGErrorSuccess) return err;
	CGConfigureDisplayOrigin(cfg, id, x, y);
	return CGCompleteDisplayConfiguration(cfg, kCGConfigureForSession);
}
*/
import "C"
import (
	"fmt"
	"unsafe"
)

// VirtualDisplay represents a macOS virtual display created via the
// private CGVirtualDisplay API. The display appears as a real extended
// monitor to the system.
type VirtualDisplay struct {
	ptr       unsafe.Pointer
	displayID uint32
	width     int
	height    int
}

// Config holds the parameters for creating a virtual display.
type Config struct {
	Width  int
	Height int
	PPI    int
	HiDPI  bool
}

// New creates a new virtual display with the given configuration.
// The caller must call Close when the display is no longer needed.
func New(cfg Config) (*VirtualDisplay, error) {
	ccfg := C.VirtualDisplayConfig{
		width:  C.int(cfg.Width),
		height: C.int(cfg.Height),
		ppi:    C.int(cfg.PPI),
	}
	if cfg.HiDPI {
		ccfg.hiDPI = 1
	}

	result := C.CreateVirtualDisplay(ccfg)
	if result.success == 0 {
		return nil, fmt.Errorf("create virtual display: %s", C.GoString(&result.errorMsg[0]))
	}

	return &VirtualDisplay{
		ptr:       result.display,
		displayID: uint32(result.displayID),
		width:     cfg.Width,
		height:    cfg.Height,
	}, nil
}

// DisplayID returns the CGDirectDisplayID assigned to this virtual display.
func (d *VirtualDisplay) DisplayID() uint32 {
	return d.displayID
}

// Width returns the pixel width of the virtual display.
func (d *VirtualDisplay) Width() int {
	return d.width
}

// Height returns the pixel height of the virtual display.
func (d *VirtualDisplay) Height() int {
	return d.height
}

// Origin returns the display's current top-left position in global coordinates.
func (d *VirtualDisplay) Origin() (x, y int) {
	b := C.CGDisplayBounds(C.CGDirectDisplayID(d.displayID))
	return int(b.origin.x), int(b.origin.y)
}

// SetOrigin moves the display to the given global position (macOS snaps it
// to an edge of the existing arrangement if it would not touch another display).
func (d *VirtualDisplay) SetOrigin(x, y int) error {
	if err := C.SetDisplayOrigin(C.CGDirectDisplayID(d.displayID), C.int(x), C.int(y)); err != C.kCGErrorSuccess {
		return fmt.Errorf("configure display origin: CGError %d", int(err))
	}
	return nil
}

// Close destroys the virtual display and releases all associated resources.
func (d *VirtualDisplay) Close() {
	if d.ptr != nil {
		C.DestroyVirtualDisplay(d.ptr)
		d.ptr = nil
	}
}
