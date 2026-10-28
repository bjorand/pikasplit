//go:build darwin

// Package mainloop keeps the main thread servicing the CoreFoundation run
// loop (and with it the main dispatch queue), which macOS display APIs
// expect, while the program runs on other goroutines.
package mainloop

/*
#cgo LDFLAGS: -framework CoreFoundation
#include <CoreFoundation/CoreFoundation.h>

static volatile int mlStopped;

static void mlNoop(CFRunLoopTimerRef t, void *info) {}

static void mlRun(void) {
	// A run loop without sources returns immediately; a far-future timer
	// keeps it waiting.
	CFRunLoopTimerRef t = CFRunLoopTimerCreate(NULL, CFAbsoluteTimeGetCurrent() + 1e9, 1e9, 0, 0, mlNoop, NULL);
	CFRunLoopAddTimer(CFRunLoopGetCurrent(), t, kCFRunLoopCommonModes);
	while (!__atomic_load_n(&mlStopped, __ATOMIC_SEQ_CST)) {
		CFRunLoopRunInMode(kCFRunLoopDefaultMode, 1.0, false);
	}
	CFRunLoopRemoveTimer(CFRunLoopGetCurrent(), t, kCFRunLoopCommonModes);
	CFRelease(t);
}

static void mlStop(void) {
	__atomic_store_n(&mlStopped, 1, __ATOMIC_SEQ_CST);
	CFRunLoopStop(CFRunLoopGetMain());
}
*/
import "C"

import "runtime"

// Package init runs on the main thread; keep the main goroutine there.
func init() { runtime.LockOSThread() }

// Run calls fn on a new goroutine and services the main run loop until fn
// returns. It must be called from main.
func Run(fn func() error) error {
	errc := make(chan error, 1)
	go func() {
		errc <- fn()
		C.mlStop()
	}()
	C.mlRun()
	return <-errc
}
