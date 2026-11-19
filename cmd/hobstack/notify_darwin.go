//go:build darwin && cgo

package main

// Notificarile de sistem pe macOS. Din HobStack.app (are bundle id app.hobstack): UNUserNotificationCenter, cu clic
// spre proiect (prima data macOS cere permisiunea). Binarul pornit direct nu are bundle (UNUserNotificationCenter
// ar opri procesul): osascriptNotify, fara clic. Codul Objective-C sta aici (fara //export); goNotifyClick e in
// notify_exports_darwin.go.

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Foundation -framework UserNotifications
#import <Foundation/Foundation.h>
#import <UserNotifications/UserNotifications.h>
#include <stdlib.h>

extern void goNotifyClick(char *id);

@interface HSNotifyDelegate : NSObject <UNUserNotificationCenterDelegate>
@end
@implementation HSNotifyDelegate
// si cu HobStack in fata notificarea apare (banner); fara asta macOS o ascunde
- (void)userNotificationCenter:(UNUserNotificationCenter *)center willPresentNotification:(UNNotification *)notification
	withCompletionHandler:(void (^)(UNNotificationPresentationOptions))completionHandler {
	completionHandler(UNNotificationPresentationOptionBanner | UNNotificationPresentationOptionList);
}
- (void)userNotificationCenter:(UNUserNotificationCenter *)center didReceiveNotificationResponse:(UNNotificationResponse *)response
	withCompletionHandler:(void (^)(void))completionHandler {
	if ([response.actionIdentifier isEqualToString:UNNotificationDefaultActionIdentifier]) {
		goNotifyClick((char *)response.notification.request.identifier.UTF8String);
	}
	completionHandler();
}
@end

static HSNotifyDelegate *hsNotifyDelegate;

// hsInBundle: 1 daca ruleaza din HobStack.app (bundle cu identificator)
static int hsInBundle(void) {
	NSBundle *b = [NSBundle mainBundle];
	return [b.bundlePath hasSuffix:@".app"] && b.bundleIdentifier.length > 0;
}

// hsNotifySetup: delegatul si cererea de permisiune, o data
static void hsNotifySetup(void) {
	UNUserNotificationCenter *c = [UNUserNotificationCenter currentNotificationCenter];
	hsNotifyDelegate = [HSNotifyDelegate new];
	c.delegate = hsNotifyDelegate;
	[c requestAuthorizationWithOptions:(UNAuthorizationOptionAlert | UNAuthorizationOptionSound)
		completionHandler:^(BOOL granted, NSError *error) {
			if (!granted) NSLog(@"HobStack notifications are not allowed (System Settings > Notifications)");
		}];
}

static void hsNotify(const char *ident, const char *title, const char *body) {
	UNMutableNotificationContent *content = [UNMutableNotificationContent new];
	content.title = [NSString stringWithUTF8String:title];
	content.body = [NSString stringWithUTF8String:body];
	content.sound = [UNNotificationSound defaultSound];
	NSString *i = [NSString stringWithUTF8String:ident];
	UNNotificationRequest *req = [UNNotificationRequest requestWithIdentifier:i content:content trigger:nil];
	[[UNUserNotificationCenter currentNotificationCenter] addNotificationRequest:req withCompletionHandler:^(NSError *error) {
		if (error != nil) NSLog(@"HobStack notification: %@", error.localizedDescription);
	}];
}
*/
import "C"

import (
	"strconv"
	"sync"
	"unsafe"
)

// clicks: ce face clic-ul pe fiecare notificare trimisa (dupa identificator); pastram ultimele cateva.
var clicks struct {
	sync.Mutex
	next int
	fn   map[string]func()
}

var notifySetup = sync.OnceValue(func() bool {
	if C.hsInBundle() == 0 {
		return false
	}
	C.hsNotifySetup()
	return true
})

func sendNotification(title, body string, onClick func()) {
	if !notifySetup() {
		osascriptNotify(title, body)
		return
	}
	clicks.Lock()
	clicks.next++
	id := "hobstack-" + strconv.Itoa(clicks.next)
	if clicks.fn == nil {
		clicks.fn = map[string]func(){}
	}
	clicks.fn[id] = onClick
	delete(clicks.fn, "hobstack-"+strconv.Itoa(clicks.next-50)) // cele vechi nu mai sunt pe ecran
	clicks.Unlock()
	ci, ct, cb := C.CString(id), C.CString(title), C.CString(body)
	defer C.free(unsafe.Pointer(ci))
	defer C.free(unsafe.Pointer(ct))
	defer C.free(unsafe.Pointer(cb))
	C.hsNotify(ci, ct, cb)
}

// notifyClicked: chemat de goNotifyClick (firul principal); munca merge in goroutine.
func notifyClicked(id string) {
	clicks.Lock()
	fn := clicks.fn[id]
	clicks.Unlock()
	if fn != nil {
		go fn()
	}
}
