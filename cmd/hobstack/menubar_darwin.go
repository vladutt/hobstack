//go:build darwin && cgo

package main

// Bara de meniu pe macOS (H-ul de langa ceas): NSStatusItem cu meniul Open HobStack / Quit HobStack.
// Cocoa cere firul principal: init blocheaza goroutine-ul main pe el, main ruleaza aplicatia (start) intr-o
// goroutine si bucla Cocoa ([NSApp run]) pe firul principal pana la stopMenuBar. Fara iconita in Dock
// (NSApplicationActivationPolicyAccessory). Codul Objective-C sta aici, in preambul (fara //export in fisierul
// asta); functiile chemate din meniu sunt in menubar_exports_darwin.go.

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>
#include <stdlib.h>

extern void goMenuOpen(void);
extern void goMenuQuit(void);

@interface HSMenuTarget : NSObject
@end
@implementation HSMenuTarget
- (void)open:(id)sender { goMenuOpen(); }
- (void)quit:(id)sender { goMenuQuit(); }
@end

static NSStatusItem *hsItem;
static HSMenuTarget *hsTarget;

static void hsRunApp(void) {
	@autoreleasepool {
		[NSApplication sharedApplication];
		[NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
		[NSApp run];
	}
}

static void hsShowStatusItem(const void *png, int len) {
	NSData *data = [NSData dataWithBytes:png length:len]; // copia, inainte de dispatch
	dispatch_async(dispatch_get_main_queue(), ^{
		if (hsItem != nil) return;
		hsTarget = [HSMenuTarget new];
		hsItem = [[NSStatusBar systemStatusBar] statusItemWithLength:NSSquareStatusItemLength];
		NSImage *img = [[NSImage alloc] initWithData:data];
		[img setSize:NSMakeSize(18, 18)];
		[img setTemplate:YES]; // alb / negru dupa tema barii
		hsItem.button.image = img;
		hsItem.button.toolTip = @"HobStack";
		NSMenu *menu = [NSMenu new];
		NSMenuItem *open = [[NSMenuItem alloc] initWithTitle:@"Open HobStack" action:@selector(open:) keyEquivalent:@""];
		open.target = hsTarget;
		[menu addItem:open];
		[menu addItem:[NSMenuItem separatorItem]];
		NSMenuItem *quit = [[NSMenuItem alloc] initWithTitle:@"Quit HobStack" action:@selector(quit:) keyEquivalent:@"q"];
		quit.target = hsTarget;
		[menu addItem:quit];
		hsItem.menu = menu;
	});
}

// hsConfirm: se cheama din meniu, deci pe firul principal. 1 = Quit.
static int hsConfirm(const char *text) {
	NSAlert *a = [NSAlert new];
	a.messageText = @"Quit HobStack?";
	a.informativeText = [NSString stringWithUTF8String:text];
	[a addButtonWithTitle:@"Quit"];
	[a addButtonWithTitle:@"Cancel"];
	[NSApp activateIgnoringOtherApps:YES];
	return [a runModal] == NSAlertFirstButtonReturn ? 1 : 0;
}

// hsStopApp scoate iconita si opreste bucla; [NSApp stop:] are efect abia dupa un eveniment, deci trimitem unul.
static void hsStopApp(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		if (hsItem != nil) {
			[[NSStatusBar systemStatusBar] removeStatusItem:hsItem];
			hsItem = nil;
		}
		[NSApp stop:nil];
		NSEvent *e = [NSEvent otherEventWithType:NSEventTypeApplicationDefined location:NSZeroPoint modifierFlags:0
			timestamp:0 windowNumber:0 context:nil subtype:0 data1:0 data2:0];
		[NSApp postEvent:e atStart:YES];
	});
}
*/
import "C"

import (
	_ "embed"
	"fmt"
	"runtime"
	"sync"
	"unsafe"
)

func init() { runtime.LockOSThread() } // goroutine-ul main ramane pe firul principal (cerut de Cocoa)

//go:embed menubar.png
var menubarPNG []byte

// menuHooks: ce fac Open / Quit; le pune startTray dupa ce HobStack asculta pe port.
var menuHooks struct {
	sync.Mutex
	open, quit func()
	active     func() int
}

// menuBarAvailable: pe macOS (build cu cgo) start ruleaza cu bara de meniu.
func menuBarAvailable() bool { return true }

// runMenuBar ruleaza bucla Cocoa pe firul principal pana la stopMenuBar.
func runMenuBar() { C.hsRunApp() }

func stopMenuBar() { C.hsStopApp() }

type tray struct{}

// startTray pune H-ul in bara de meniu (cand HobStack e gata); iconita dispare la stopMenuBar.
func startTray(open, quit func(), active func() int) *tray {
	menuHooks.Lock()
	menuHooks.open, menuHooks.quit, menuHooks.active = open, quit, active
	menuHooks.Unlock()
	C.hsShowStatusItem(unsafe.Pointer(&menubarPNG[0]), C.int(len(menubarPNG)))
	return &tray{}
}

func (*tray) Close() {}

// confirmQuit: cu agenti care lucreaza, intreaba (pe firul principal, chemat din meniu).
func confirmQuit(n int) bool {
	text := C.CString(fmt.Sprintf("%d agent(s) are working right now. Their tasks go back to Queued and continue the next time HobStack starts.", n))
	defer C.free(unsafe.Pointer(text))
	return C.hsConfirm(text) == 1
}
