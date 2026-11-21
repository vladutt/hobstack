//go:build darwin && cgo

package main

// Folderele si fisierele deschise in Finder din HobStack (link-urile din chat, Show in Finder): direct din aplicatie,
// prin NSWorkspace, dupa ce HobStack ii cedeaza lui Finder locul din fata (yieldActivationToApplication, macOS 14+).
// Cu `open` pornit din fundal, Finder devenea activ, dar fereastra lui ramanea sub fereastra HobStack.

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>
#include <stdlib.h>

static void hsFinderOpen(const char *p, int reveal) {
	NSString *path = [NSString stringWithUTF8String:p]; // copia, inainte de dispatch (C.free dupa apel)
	dispatch_async(dispatch_get_main_queue(), ^{
		NSURL *u = [NSURL fileURLWithPath:path];
		if (@available(macOS 14.0, *)) {
			[NSApp yieldActivationToApplicationWithBundleIdentifier:@"com.apple.finder"];
		}
		NSWorkspace *ws = [NSWorkspace sharedWorkspace];
		if (reveal) {
			[ws activateFileViewerSelectingURLs:@[u]];
			return;
		}
		NSURL *finder = [ws URLForApplicationWithBundleIdentifier:@"com.apple.finder"];
		NSWorkspaceOpenConfiguration *c = [NSWorkspaceOpenConfiguration configuration];
		c.activates = YES;
		if (finder != nil) {
			[ws openURLs:@[u] withApplicationAtURL:finder configuration:c completionHandler:nil];
		} else {
			[ws openURL:u configuration:c completionHandler:nil];
		}
	});
}
*/
import "C"

import (
	"os"
	"unsafe"

	"hobstack/internal/opener"
)

func init() {
	opener.Native = func(path string, reveal bool) error {
		if _, err := os.Stat(path); err != nil {
			return err // ca `open`: o cale care nu exista e o eroare, nu o fereastra goala
		}
		cp := C.CString(path)
		defer C.free(unsafe.Pointer(cp))
		C.hsFinderOpen(cp, C.int(map[bool]int{false: 0, true: 1}[reveal]))
		return nil
	}
}
