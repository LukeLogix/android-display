package input

/*
#cgo CFLAGS: -x objective-c -I../../bridge
#cgo LDFLAGS: -framework CoreGraphics -framework ApplicationServices
#include "input_bridge.h"
*/
import "C"

import "log"

// DisplayBounds represents the pixel bounds of a display.
type DisplayBounds struct {
	X, Y, Width, Height float64
}

// Injector translates normalised touch coordinates into macOS CGEvent
// mouse actions and posts them via the Quartz Event Services API.
type Injector struct {
	displayID uint32
}

// NewInjector creates a new Injector targeting the given display.
// It logs a warning if Accessibility permission has not been granted.
func NewInjector(displayID uint32) *Injector {
	if !bool(C.CheckAccessibilityPermission()) {
		log.Println("WARNING: Accessibility permission not granted!")
		log.Println("Go to System Preferences → Privacy & Security → Accessibility")
		log.Println("and add this application to enable touch input.")
	}

	inj := &Injector{displayID: displayID}
	b := inj.bounds()
	log.Printf("injector: display 0x%x bounds: (%.0f,%.0f) %.0fx%.0f",
		displayID, b.X, b.Y, b.Width, b.Height)
	return inj
}

// bounds returns the display's current global bounds. It is queried per event
// because macOS may move the display after creation (restoring the saved
// arrangement, or the user rearranging it in System Settings).
func (inj *Injector) bounds() DisplayBounds {
	cb := C.GetDisplayBounds(C.uint32_t(inj.displayID))
	return DisplayBounds{
		X:      float64(cb.x),
		Y:      float64(cb.y),
		Width:  float64(cb.width),
		Height: float64(cb.height),
	}
}

// ToScreenCoords converts normalised (0..1) coordinates to absolute
// screen coordinates within the display's current bounds.
func (inj *Injector) ToScreenCoords(nx, ny float32) (float64, float64) {
	b := inj.bounds()
	return b.X + float64(nx)*b.Width, b.Y + float64(ny)*b.Height
}

// MouseMove posts a mouse-moved event at the normalised coordinates.
func (inj *Injector) MouseMove(nx, ny float32) {
	x, y := inj.ToScreenCoords(nx, ny)
	C.InjectMouseMove(C.double(x), C.double(y))
}

// LeftMouseDown posts a left-mouse-down event at the normalised coordinates.
func (inj *Injector) LeftMouseDown(nx, ny float32) {
	x, y := inj.ToScreenCoords(nx, ny)
	C.InjectLeftMouseDown(C.double(x), C.double(y))
}

// LeftMouseUp posts a left-mouse-up event at the normalised coordinates.
func (inj *Injector) LeftMouseUp(nx, ny float32) {
	x, y := inj.ToScreenCoords(nx, ny)
	C.InjectLeftMouseUp(C.double(x), C.double(y))
}

// LeftMouseDragged posts a left-mouse-dragged event at the normalised coordinates.
func (inj *Injector) LeftMouseDragged(nx, ny float32) {
	x, y := inj.ToScreenCoords(nx, ny)
	C.InjectLeftMouseDragged(C.double(x), C.double(y))
}

// RightMouseDown posts a right-mouse-down event at the normalised coordinates.
func (inj *Injector) RightMouseDown(nx, ny float32) {
	x, y := inj.ToScreenCoords(nx, ny)
	C.InjectRightMouseDown(C.double(x), C.double(y))
}

// RightMouseUp posts a right-mouse-up event at the normalised coordinates.
func (inj *Injector) RightMouseUp(nx, ny float32) {
	x, y := inj.ToScreenCoords(nx, ny)
	C.InjectRightMouseUp(C.double(x), C.double(y))
}

// ScrollWheel posts a scroll-wheel event with the given pixel deltas.
func (inj *Injector) ScrollWheel(deltaX, deltaY int32) {
	C.InjectScrollWheel(C.int32_t(deltaX), C.int32_t(deltaY))
}

// TabletProximityEnter posts a tablet proximity-enter event.
func (inj *Injector) TabletProximityEnter() {
	C.InjectTabletProximityEnter()
}

// TabletProximityLeave posts a tablet proximity-leave event.
func (inj *Injector) TabletProximityLeave() {
	C.InjectTabletProximityLeave()
}

// TabletDown posts a tablet left-mouse-down event with pressure and tilt.
func (inj *Injector) TabletDown(nx, ny float32, pressure float32, tiltX, tiltY float32) {
	x, y := inj.ToScreenCoords(nx, ny)
	C.InjectTabletDown(C.double(x), C.double(y), C.double(pressure), C.double(tiltX), C.double(tiltY))
}

// TabletUp posts a tablet left-mouse-up event with pressure and tilt.
func (inj *Injector) TabletUp(nx, ny float32, pressure float32, tiltX, tiltY float32) {
	x, y := inj.ToScreenCoords(nx, ny)
	C.InjectTabletUp(C.double(x), C.double(y), C.double(pressure), C.double(tiltX), C.double(tiltY))
}

// TabletDragged posts a tablet left-mouse-dragged event with pressure and tilt.
func (inj *Injector) TabletDragged(nx, ny float32, pressure float32, tiltX, tiltY float32) {
	x, y := inj.ToScreenCoords(nx, ny)
	C.InjectTabletDragged(C.double(x), C.double(y), C.double(pressure), C.double(tiltX), C.double(tiltY))
}

// TabletMoved posts a tablet mouse-moved event with pressure and tilt.
func (inj *Injector) TabletMoved(nx, ny float32, pressure float32, tiltX, tiltY float32) {
	x, y := inj.ToScreenCoords(nx, ny)
	C.InjectTabletMoved(C.double(x), C.double(y), C.double(pressure), C.double(tiltX), C.double(tiltY))
}
