//go:build darwin

// Package vdisplay creates a virtual display on macOS through the private
// CGVirtualDisplay API (the approach used by DeskPad and BetterDisplay). The
// display appears in System Settings > Displays like a real monitor and can
// be resized while in use.
//
// Being private, the API may change between macOS releases; the classes are
// looked up at runtime so a missing API is an error, not a crash.
package vdisplay

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Foundation -framework CoreGraphics
#include <stdlib.h>
#import <Foundation/Foundation.h>
#import <CoreGraphics/CoreGraphics.h>

@interface CGVirtualDisplayDescriptor : NSObject
@property (retain, nonatomic) NSString *name;
@property (nonatomic) unsigned int maxPixelsHigh;
@property (nonatomic) unsigned int maxPixelsWide;
@property (nonatomic) CGSize sizeInMillimeters;
@property (nonatomic) unsigned int serialNum;
@property (nonatomic) unsigned int productID;
@property (nonatomic) unsigned int vendorID;
@property (copy, nonatomic) void (^terminationHandler)(id, id);
- (void)setDispatchQueue:(dispatch_queue_t)queue;
@end

@interface CGVirtualDisplayMode : NSObject
- (instancetype)initWithWidth:(unsigned int)width height:(unsigned int)height refreshRate:(double)refreshRate;
@end

@interface CGVirtualDisplaySettings : NSObject
@property (retain, nonatomic) NSArray *modes;
@property (nonatomic) unsigned int hiDPI;
@end

@interface CGVirtualDisplay : NSObject
@property (readonly, nonatomic) unsigned int displayID;
- (instancetype)initWithDescriptor:(CGVirtualDisplayDescriptor *)descriptor;
- (BOOL)applySettings:(CGVirtualDisplaySettings *)settings;
@end

static dispatch_queue_t vdQueue;

// Returns a retained CGVirtualDisplay, or NULL.
static void *vdCreate(const char *name, unsigned int maxW, unsigned int maxH, unsigned int serial) {
	@autoreleasepool {
		Class descC = NSClassFromString(@"CGVirtualDisplayDescriptor");
		Class dispC = NSClassFromString(@"CGVirtualDisplay");
		if (descC == nil || dispC == nil) return NULL;
		if (vdQueue == NULL) vdQueue = dispatch_queue_create("pikasplit.vdisplay", DISPATCH_QUEUE_SERIAL);

		CGVirtualDisplayDescriptor *d = [[descC alloc] init];
		if ([d respondsToSelector:@selector(setDispatchQueue:)]) [d setDispatchQueue:vdQueue];
		d.name = [NSString stringWithUTF8String:name];
		d.maxPixelsWide = maxW;
		d.maxPixelsHigh = maxH;
		// About 160 pixels per inch, a typical 4K desktop monitor.
		d.sizeInMillimeters = CGSizeMake(maxW * 25.4 / 160.0, maxH * 25.4 / 160.0);
		// Stable IDs so macOS remembers the display arrangement.
		d.vendorID = 0x7069;
		d.productID = 0x6B61;
		d.serialNum = serial;
		d.terminationHandler = ^(id a, id b) {};

		CGVirtualDisplay *v = [[dispC alloc] initWithDescriptor:d];
		[d release];
		return v;
	}
}

static int vdApply(void *disp, unsigned int w, unsigned int h, int hiDPI, double hz) {
	@autoreleasepool {
		Class setC = NSClassFromString(@"CGVirtualDisplaySettings");
		Class modeC = NSClassFromString(@"CGVirtualDisplayMode");
		if (setC == nil || modeC == nil) return 0;
		CGVirtualDisplaySettings *s = [[setC alloc] init];
		CGVirtualDisplayMode *m = [[modeC alloc] initWithWidth:w height:h refreshRate:hz];
		s.modes = @[ m ];
		s.hiDPI = hiDPI ? 1 : 0;
		BOOL ok = [(CGVirtualDisplay *)disp applySettings:s];
		[m release];
		[s release];
		return ok ? 1 : 0;
	}
}

static unsigned int vdID(void *disp) { return ((CGVirtualDisplay *)disp).displayID; }

static void vdRelease(void *disp) { [(CGVirtualDisplay *)disp release]; }
*/
import "C"

import (
	"errors"
	"fmt"
	"unsafe"
)

const refreshRate = 60

// Display is a virtual display. It disappears on Close or when the process
// exits.
type Display struct {
	h          unsafe.Pointer
	maxW, maxH int
}

// New creates a virtual display whose backing store can grow up to
// maxW x maxH pixels (fixed for the display's lifetime). It has no mode
// until SetMode is called.
func New(name string, maxW, maxH int) (*Display, error) {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	h := C.vdCreate(cname, C.uint(maxW), C.uint(maxH), 1)
	if h == nil {
		return nil, errors.New("vdisplay: CGVirtualDisplay unavailable on this macOS version")
	}
	return &Display{h: h, maxW: maxW, maxH: maxH}, nil
}

// SetMode switches the display to w x h points. With hiDPI each point is
// backed by 2x2 pixels, which must fit in the maximum size given to New.
func (d *Display) SetMode(w, h int, hiDPI bool) error {
	px := 1
	if hiDPI {
		px = 2
	}
	if w <= 0 || h <= 0 || w*px > d.maxW || h*px > d.maxH {
		return fmt.Errorf("vdisplay: mode %dx%d (hiDPI %v) exceeds %dx%d pixels", w, h, hiDPI, d.maxW, d.maxH)
	}
	hi := C.int(0)
	if hiDPI {
		hi = 1
	}
	if C.vdApply(d.h, C.uint(w), C.uint(h), hi, refreshRate) == 0 {
		return fmt.Errorf("vdisplay: macOS refused mode %dx%d (hiDPI %v)", w, h, hiDPI)
	}
	return nil
}

// ID is the CGDirectDisplayID of the display.
func (d *Display) ID() uint32 { return uint32(C.vdID(d.h)) }

// Close removes the display.
func (d *Display) Close() {
	if d.h != nil {
		C.vdRelease(d.h)
		d.h = nil
	}
}
