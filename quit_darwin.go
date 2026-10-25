package main

/*
#cgo CFLAGS: -x objective-c
#import <AppKit/AppKit.h>
#include <objc/runtime.h>

extern int nibMayQuit(void);

static NSApplicationTerminateReply nibShouldTerminate(id self, SEL _cmd, NSApplication *app) {
	return nibMayQuit() ? NSTerminateNow : NSTerminateCancel;
}

// nibHookQuit adds applicationShouldTerminate: to Gio's app delegate.
static void nibHookQuit(void) {
	Class c = objc_getClass("GioAppDelegate");
	if (c != Nil) {
		class_addMethod(c, @selector(applicationShouldTerminate:), (IMP)nibShouldTerminate, "Q@:@");
	}
}

static int nibQuitHooked(void) {
	Class c = objc_getClass("GioAppDelegate");
	return c != Nil && class_respondsToSelector(c, @selector(applicationShouldTerminate:));
}
*/
import "C"

// ⌘Q goes through the app menu, which quits without asking the window, so
// the app delegate is taught to ask first.
func init() { C.nibHookQuit() }

// quitHooked reports whether the app delegate asks before quitting.
func quitHooked() bool { return C.nibQuitHooked() != 0 }
