//go:build windows

// Package winui is the viewer window, written against the Win32 API directly
// (no cgo). While the window is in the foreground, a low-level keyboard hook
// captures every key, including Win, Alt+Tab and Alt+F4, and forwards it;
// Ctrl+Alt+End suspends capture until the window is clicked again.
//
// All callbacks run on the UI thread and must not block.
package winui

import (
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"github.com/bjorand/pikasplit/internal/keymap"
	"github.com/bjorand/pikasplit/internal/proto"
)

// Events receives input captured by the window.
type Events struct {
	Key    func(hid uint16, down bool)
	Mouse  func(x, y int32, buttons uint8) // client pixels
	Scroll func(dx, dy int16)              // WHEEL_DELTA units
	Resize func(w, h, dpi int)             // client pixels, debounced
	// Release is called when input stops going to the Mac (focus lost or
	// capture suspended): the receiver must lift held keys and buttons.
	Release func()
}

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	pRegisterClassExW              = user32.NewProc("RegisterClassExW")
	pCreateWindowExW               = user32.NewProc("CreateWindowExW")
	pDefWindowProcW                = user32.NewProc("DefWindowProcW")
	pDestroyWindow                 = user32.NewProc("DestroyWindow")
	pGetMessageW                   = user32.NewProc("GetMessageW")
	pTranslateMessage              = user32.NewProc("TranslateMessage")
	pDispatchMessageW              = user32.NewProc("DispatchMessageW")
	pPostQuitMessage               = user32.NewProc("PostQuitMessage")
	pPostMessageW                  = user32.NewProc("PostMessageW")
	pShowWindow                    = user32.NewProc("ShowWindow")
	pLoadCursorW                   = user32.NewProc("LoadCursorW")
	pSetWindowsHookExW             = user32.NewProc("SetWindowsHookExW")
	pUnhookWindowsHookEx           = user32.NewProc("UnhookWindowsHookEx")
	pCallNextHookEx                = user32.NewProc("CallNextHookEx")
	pGetForegroundWindow           = user32.NewProc("GetForegroundWindow")
	pBeginPaint                    = user32.NewProc("BeginPaint")
	pEndPaint                      = user32.NewProc("EndPaint")
	pFillRect                      = user32.NewProc("FillRect")
	pSetCursor                     = user32.NewProc("SetCursor")
	pStretchDIBits                 = gdi32.NewProc("StretchDIBits")
	pSetStretchBltMode             = gdi32.NewProc("SetStretchBltMode")
	pCreateFontW                   = gdi32.NewProc("CreateFontW")
	pSelectObject                  = gdi32.NewProc("SelectObject")
	pDrawTextW                     = user32.NewProc("DrawTextW")
	pGetClientRect                 = user32.NewProc("GetClientRect")
	pInvalidateRect                = user32.NewProc("InvalidateRect")
	pSetTimer                      = user32.NewProc("SetTimer")
	pKillTimer                     = user32.NewProc("KillTimer")
	pSetCapture                    = user32.NewProc("SetCapture")
	pReleaseCapture                = user32.NewProc("ReleaseCapture")
	pSetWindowTextW                = user32.NewProc("SetWindowTextW")
	pSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
	pGetDpiForWindow               = user32.NewProc("GetDpiForWindow")
	pCreateSolidBrush              = gdi32.NewProc("CreateSolidBrush")
	pSetTextColor                  = gdi32.NewProc("SetTextColor")
	pSetBkMode                     = gdi32.NewProc("SetBkMode")
	pGetModuleHandleW              = kernel32.NewProc("GetModuleHandleW")
	pVirtualAlloc                  = kernel32.NewProc("VirtualAlloc")
	pVirtualFree                   = kernel32.NewProc("VirtualFree")
)

const (
	wmDestroy       = 0x0002
	wmSize          = 0x0005
	wmKillFocus     = 0x0008
	wmPaint         = 0x000F
	wmEraseBkgnd    = 0x0014
	wmTimer         = 0x0113
	wmMouseMove     = 0x0200
	wmLButtonDown   = 0x0201
	wmLButtonUp     = 0x0202
	wmRButtonDown   = 0x0204
	wmRButtonUp     = 0x0205
	wmMButtonDown   = 0x0207
	wmMButtonUp     = 0x0208
	wmMouseWheel    = 0x020A
	wmXButtonDown   = 0x020B
	wmXButtonUp     = 0x020C
	wmMouseHWheel   = 0x020E
	wmMouseActivate = 0x0021
	wmSetCursor     = 0x0020
	wmApp           = 0x8000
	wmAppFrame      = 0x8001

	htClient     = 1
	srcCopy      = 0x00CC0020
	colorOnColor = 3

	maActivateAndEat = 2

	mkLButton  = 0x0001
	mkRButton  = 0x0002
	mkMButton  = 0x0010
	mkXButton1 = 0x0020
	mkXButton2 = 0x0040

	whKeyboardLL   = 13
	llkhfExtended  = 0x01
	llkhfInjected  = 0x10
	llkhfUp        = 0x80
	vkPause        = 0x13
	vkNumLock      = 0x90
	wsOverlapped   = 0x00CF0000 // WS_OVERLAPPEDWINDOW
	cwUseDefault   = 0x80000000
	swShow         = 5
	idcArrow       = 32512
	resizeTimerID  = 1
	resizeDebounce = 300 // ms

	dtCenter    = 0x01
	dtWordBreak = 0x10
	dtCalcRect  = 0x400
	transparent = 1
)

// HID usages used by the suspend hotkey.
const (
	hidEnd       = 0x4D
	hidLeftCtrl  = 0xE0
	hidLeftAlt   = 0xE2
	hidRightCtrl = 0xE4
	hidRightAlt  = 0xE6
)

type wndClassEx struct {
	size       uint32
	style      uint32
	wndProc    uintptr
	clsExtra   int32
	wndExtra   int32
	instance   uintptr
	icon       uintptr
	cursor     uintptr
	background uintptr
	menuName   *uint16
	className  *uint16
	iconSm     uintptr
}

type point struct{ x, y int32 }

type msg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
	private uint32
}

type rect struct{ left, top, right, bottom int32 }

type paintStruct struct {
	hdc       uintptr
	erase     int32
	paint     rect
	restore   int32
	incUpdate int32
	reserved  [32]byte
}

type kbdLLHookStruct struct {
	vkCode    uint32
	scanCode  uint32
	flags     uint32
	time      uint32
	extraInfo uintptr
}

// Window state. There is only ever one viewer window per process.
var (
	ev        Events
	hwnd      uintptr
	brush     uintptr
	suspended bool
	pressed   = map[uint16]bool{} // HID usages forwarded and still down
	width     int
	height    int

	statusMu sync.Mutex
	status   string
	title    string

	font    uintptr // status text font, created for the window DPI
	fontDPI uintptr

	frameMu sync.Mutex
	frame   []byte // top-down BGRA, nil when there is no video
	frameW  int
	frameH  int
)

type bitmapInfoHeader struct {
	size          uint32
	width         int32
	height        int32
	planes        uint16
	bitCount      uint16
	compression   uint32
	sizeImage     uint32
	xPelsPerMeter int32
	yPelsPerMeter int32
	clrUsed       uint32
	clrImportant  uint32
}

// NewFrameBuffer allocates n bytes for a picture passed to SetFrame. GDI
// refuses to paint large buffers from the Go heap (which is committed in
// several VirtualAlloc regions a large object can straddle), so frames live
// in a single VirtualAlloc region. Release with FreeFrameBuffer.
func NewFrameBuffer(n int) []byte {
	const memCommitReserve, pageReadWrite = 0x3000, 0x04
	p, _, _ := pVirtualAlloc.Call(0, uintptr(n), memCommitReserve, pageReadWrite)
	if p == 0 {
		panic("winui: out of memory for a frame buffer")
	}
	return unsafe.Slice(*(**byte)(unsafe.Pointer(&p)), n)
}

// FreeFrameBuffer releases a buffer from NewFrameBuffer. The window must no
// longer reference it (see SetFrame).
func FreeFrameBuffer(b []byte) {
	const memRelease = 0x8000
	if cap(b) > 0 {
		pVirtualFree.Call(uintptr(unsafe.Pointer(&b[:1][0])), 0, memRelease)
	}
}

// SetFrame shows a w x h top-down BGRA picture from NewFrameBuffer,
// stretched to the window if sizes differ. It returns the previously shown buffer, which the caller
// may reuse: the window no longer references it. nil clears the video and
// shows the status text again. Safe to call from any goroutine.
func SetFrame(bgra []byte, w, h int) (prev []byte) {
	frameMu.Lock()
	prev, frame, frameW, frameH = frame, bgra, w, h
	frameMu.Unlock()
	if hwnd != 0 {
		pPostMessageW.Call(hwnd, wmAppFrame, 0, 0)
	}
	return prev
}

func hasFrame() bool {
	frameMu.Lock()
	defer frameMu.Unlock()
	return frame != nil
}

// SetStatus updates the text shown in the window and its title. It is safe
// to call from any goroutine.
func SetStatus(windowTitle, text string) {
	statusMu.Lock()
	title, status = windowTitle, text
	statusMu.Unlock()
	if hwnd != 0 {
		pPostMessageW.Call(hwnd, wmApp, 0, 0)
	}
}

// Run creates the window and runs its message loop until it is closed.
func Run(events Events) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ev = events

	// Per-monitor DPI awareness: client sizes are real pixels, which is what
	// the Mac display should match. -4 = PER_MONITOR_AWARE_V2.
	pSetProcessDpiAwarenessContext.Call(^uintptr(3))

	inst, _, _ := pGetModuleHandleW.Call(0)
	cursor, _, _ := pLoadCursorW.Call(0, idcArrow)
	brush, _, _ = pCreateSolidBrush.Call(0x00281E1E) // BGR
	className, _ := syscall.UTF16PtrFromString("pikasplit-view")
	wc := wndClassEx{
		wndProc:    syscall.NewCallback(wndProc),
		instance:   inst,
		cursor:     cursor,
		background: brush,
		className:  className,
	}
	wc.size = uint32(unsafe.Sizeof(wc))
	if r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return err
	}

	statusMu.Lock()
	t, _ := syscall.UTF16PtrFromString(title)
	statusMu.Unlock()
	h, _, err := pCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(t)),
		wsOverlapped, cwUseDefault, cwUseDefault, 1600, 900,
		0, 0, inst, 0)
	if h == 0 {
		return err
	}
	hwnd = h

	hook, _, err := pSetWindowsHookExW.Call(whKeyboardLL, syscall.NewCallback(keyboardHook), inst, 0)
	if hook == 0 {
		pDestroyWindow.Call(hwnd)
		return err
	}
	defer pUnhookWindowsHookEx.Call(hook)

	pShowWindow.Call(hwnd, swShow)

	var m msg
	for {
		r, _, err := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		switch int32(r) {
		case -1:
			return err
		case 0:
			return nil
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func active() bool {
	fg, _, _ := pGetForegroundWindow.Call()
	return fg == hwnd && !suspended
}

func release() {
	for k := range pressed {
		delete(pressed, k)
	}
	if ev.Release != nil {
		ev.Release()
	}
}

func keyboardHook(nCode, wParam, lParam uintptr) uintptr {
	if int32(nCode) == 0 && active() {
		k := *(**kbdLLHookStruct)(unsafe.Pointer(&lParam))
		if k.flags&llkhfInjected == 0 && handleKey(k) {
			return 1 // swallowed: Windows does not see it
		}
	}
	r, _, _ := pCallNextHookEx.Call(0, nCode, wParam, lParam)
	return r
}

func handleKey(k *kbdLLHookStruct) bool {
	var hid uint16
	var ok bool
	switch k.vkCode {
	case vkPause:
		hid, ok = keymap.HIDPause, true
	case vkNumLock:
		hid, ok = keymap.HIDNumLock, true
	default:
		hid, ok = keymap.FromPCScancode(uint16(k.scanCode), k.flags&llkhfExtended != 0)
	}
	if !ok {
		return false
	}
	down := k.flags&llkhfUp == 0

	if down && hid == hidEnd &&
		(pressed[hidLeftCtrl] || pressed[hidRightCtrl]) &&
		(pressed[hidLeftAlt] || pressed[hidRightAlt]) {
		suspended = true
		release()
		invalidate()
		return true
	}

	if down {
		pressed[hid] = true
	} else if !pressed[hid] {
		// Release of a key pressed before we had focus: let Windows have it.
		return false
	} else {
		delete(pressed, hid)
	}
	if ev.Key != nil {
		ev.Key(hid, down)
	}
	return true
}

func buttonsOf(wParam uintptr) uint8 {
	var b uint8
	if wParam&mkLButton != 0 {
		b |= proto.ButtonLeft
	}
	if wParam&mkRButton != 0 {
		b |= proto.ButtonRight
	}
	if wParam&mkMButton != 0 {
		b |= proto.ButtonMiddle
	}
	if wParam&mkXButton1 != 0 {
		b |= proto.ButtonBack
	}
	if wParam&mkXButton2 != 0 {
		b |= proto.ButtonForward
	}
	return b
}

func invalidate() { pInvalidateRect.Call(hwnd, 0, 1) }

func wndProc(h, message, wParam, lParam uintptr) uintptr {
	switch message {
	case wmMouseMove, wmLButtonDown, wmLButtonUp, wmRButtonDown, wmRButtonUp,
		wmMButtonDown, wmMButtonUp, wmXButtonDown, wmXButtonUp:
		if message == wmLButtonDown && suspended {
			// This click only resumes capture; the Mac does not see it.
			suspended = false
			invalidate()
			return 0
		}
		if !active() {
			break
		}
		buttons := buttonsOf(wParam)
		if buttons != 0 {
			pSetCapture.Call(h)
		} else {
			pReleaseCapture.Call()
		}
		if ev.Mouse != nil {
			x := int32(int16(lParam & 0xFFFF))
			y := int32(int16((lParam >> 16) & 0xFFFF))
			ev.Mouse(x, y, buttons)
		}
		if message == wmXButtonDown || message == wmXButtonUp {
			return 1
		}
		return 0
	case wmMouseWheel, wmMouseHWheel:
		if active() && ev.Scroll != nil {
			delta := int16(wParam >> 16)
			if message == wmMouseWheel {
				ev.Scroll(0, delta)
			} else {
				ev.Scroll(delta, 0)
			}
		}
		return 0
	case wmKillFocus:
		release()
		return 0
	case wmMouseActivate:
		// The click that brings the window to the front is not forwarded.
		return maActivateAndEat
	case wmSetCursor:
		// The Mac cursor is part of the video: hide ours over it.
		if lParam&0xFFFF == htClient && active() && hasFrame() {
			pSetCursor.Call(0)
			return 1
		}
	case wmSize:
		width, height = int(lParam&0xFFFF), int((lParam>>16)&0xFFFF)
		if width > 0 && height > 0 {
			pSetTimer.Call(h, resizeTimerID, resizeDebounce, 0)
		}
		invalidate()
		return 0
	case wmTimer:
		if wParam == resizeTimerID {
			pKillTimer.Call(h, resizeTimerID)
			if ev.Resize != nil {
				dpi := uintptr(96)
				if pGetDpiForWindow.Find() == nil {
					dpi, _, _ = pGetDpiForWindow.Call(h)
				}
				ev.Resize(width, height, int(dpi))
			}
		}
		return 0
	case wmApp:
		statusMu.Lock()
		t, _ := syscall.UTF16PtrFromString(title)
		statusMu.Unlock()
		pSetWindowTextW.Call(h, uintptr(unsafe.Pointer(t)))
		invalidate()
		return 0
	case wmAppFrame:
		pInvalidateRect.Call(h, 0, 0)
		return 0
	case wmEraseBkgnd:
		return 1
	case wmPaint:
		paint(h)
		return 0
	case wmDestroy:
		pPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(h, message, wParam, lParam)
	return r
}

func paint(h uintptr) {
	var ps paintStruct
	hdc, _, _ := pBeginPaint.Call(h, uintptr(unsafe.Pointer(&ps)))
	defer pEndPaint.Call(h, uintptr(unsafe.Pointer(&ps)))
	var rc rect
	pGetClientRect.Call(h, uintptr(unsafe.Pointer(&rc)))

	frameMu.Lock()
	if frame != nil {
		bi := bitmapInfoHeader{width: int32(frameW), height: -int32(frameH), planes: 1, bitCount: 32}
		bi.size = uint32(unsafe.Sizeof(bi))
		pSetStretchBltMode.Call(hdc, colorOnColor)
		pStretchDIBits.Call(hdc, 0, 0, uintptr(rc.right), uintptr(rc.bottom),
			0, 0, uintptr(frameW), uintptr(frameH),
			uintptr(unsafe.Pointer(&frame[0])), uintptr(unsafe.Pointer(&bi)), 0, srcCopy)
	}
	video := frame != nil
	frameMu.Unlock()

	statusMu.Lock()
	text := status
	statusMu.Unlock()
	if video {
		text = ""
	}
	if suspended {
		text = "Capture paused — click in the window to resume"
	}
	if !video {
		pFillRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), brush)
	}
	if text == "" {
		return
	}
	s, _ := syscall.UTF16FromString(text)
	pSelectObject.Call(hdc, statusFont(h))
	pSetTextColor.Call(hdc, 0x00E0E0E0)
	pSetBkMode.Call(hdc, transparent)
	// Wrap within margins, then center the block vertically.
	margin := rc.right / 10
	box := rect{left: margin, right: rc.right - margin}
	pDrawTextW.Call(hdc, uintptr(unsafe.Pointer(&s[0])), uintptr(len(s)-1),
		uintptr(unsafe.Pointer(&box)), dtCenter|dtWordBreak|dtCalcRect)
	textH := box.bottom
	box = rect{left: margin, right: rc.right - margin, top: (rc.bottom - textH) / 2}
	box.bottom = box.top + textH
	pDrawTextW.Call(hdc, uintptr(unsafe.Pointer(&s[0])), uintptr(len(s)-1),
		uintptr(unsafe.Pointer(&box)), dtCenter|dtWordBreak)
}

// statusFont returns an 11 pt Segoe UI font for the window's DPI.
func statusFont(h uintptr) uintptr {
	dpi := uintptr(96)
	if pGetDpiForWindow.Find() == nil {
		dpi, _, _ = pGetDpiForWindow.Call(h)
	}
	if font == 0 || dpi != fontDPI {
		face, _ := syscall.UTF16PtrFromString("Segoe UI")
		height := -int32(11 * dpi / 72)
		font, _, _ = pCreateFontW.Call(uintptr(height), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 5, 0,
			uintptr(unsafe.Pointer(face)))
		fontDPI = dpi
	}
	return font
}
