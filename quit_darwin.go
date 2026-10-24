package main

/*
#cgo CFLAGS: -x objective-c
#import <AppKit/AppKit.h>
#include <objc/runtime.h>

extern int quodMayQuit(void);

static NSApplicationTerminateReply quodShouldTerminate(id self, SEL _cmd, NSApplication *app) {
	return quodMayQuit() ? NSTerminateNow : NSTerminateCancel;
}

// quodHookQuit adds applicationShouldTerminate: to Gio's app delegate.
static void quodHookQuit(void) {
	Class c = objc_getClass("GioAppDelegate");
	if (c != Nil) {
		class_addMethod(c, @selector(applicationShouldTerminate:), (IMP)quodShouldTerminate, "Q@:@");
	}
}

static int quodQuitHooked(void) {
	Class c = objc_getClass("GioAppDelegate");
	return c != Nil && class_respondsToSelector(c, @selector(applicationShouldTerminate:));
}
*/
import "C"

// ⌘Q goes through the app menu, which quits without asking the window, so
// the app delegate is taught to ask first.
func init() { C.quodHookQuit() }

// quitHooked reports whether the app delegate asks before quitting.
func quitHooked() bool { return C.quodQuitHooked() != 0 }
