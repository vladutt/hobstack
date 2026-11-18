//go:build darwin && cgo

package main

// Fereastra proprie pe macOS: un NSWindow cu WKWebView care afiseaza UI-ul de pe 127.0.0.1 (acelasi ca in browser).
// Totul ruleaza pe firul principal (dispatch_async pe main queue), in bucla [NSApp run] din menubar_darwin.go.
// Ce da browserul gratis si facem aici: dialogurile confirm / alert / prompt, alegerea fisierelor, meniul Edit
// (fara el Cmd+C / Cmd+V nu merg), Back / Forward / zoom, linkurile externe in browserul de sistem.
// Inchisa, fereastra doar se ascunde (pagina si stream-urile raman), iar aplicatia iese din Dock.
// Codul Objective-C sta in preambul (fara //export aici); functiile chemate din Objective-C sunt in
// window_exports_darwin.go.

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework WebKit
#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
#include <stdlib.h>

extern void goWindowClosed(double x, double y, double w, double h, double zoom);
extern void goWindowReopen(void);
extern void goAppTerminate(void);
extern void goMenuQuit(void);

static int hsVisible;          // fereastra e pe ecran (scris doar pe firul principal)
static int hsTerminating;      // applicationShouldTerminate a intors NSTerminateLater

@interface HSWindowCtl : NSObject <NSWindowDelegate, WKNavigationDelegate, WKUIDelegate>
@property (strong) NSWindow *window;
@property (strong) WKWebView *web;
@property (copy) NSString *origin;
@end

static NSString *hsOriginOf(NSURL *u) {
	if (u.scheme == nil || u.host == nil) return @"";
	NSNumber *port = u.port;
	if (port == nil) return [NSString stringWithFormat:@"%@://%@", u.scheme.lowercaseString, u.host.lowercaseString];
	return [NSString stringWithFormat:@"%@://%@:%@", u.scheme.lowercaseString, u.host.lowercaseString, port];
}

static NSMenuItem *hsItem(NSString *title, SEL action, NSString *key, id target) {
	NSMenuItem *it = [[NSMenuItem alloc] initWithTitle:title action:action keyEquivalent:key];
	it.target = target;
	return it;
}

@implementation HSWindowCtl

// frame: x,y,w,h salvate (w <= 0 = nimic salvat); folosit doar daca ramane vizibil pe un ecran
- (NSRect)initialFrameX:(double)x y:(double)y w:(double)w h:(double)h {
	NSRect saved = NSMakeRect(x, y, w, h);
	if (w >= 400 && h >= 300) {
		for (NSScreen *s in [NSScreen screens]) {
			NSRect in = NSIntersectionRect(saved, s.visibleFrame);
			if (in.size.width >= 100 && in.size.height >= 100) return saved;
		}
	}
	NSRect vis = [NSScreen mainScreen].visibleFrame;
	CGFloat fw = MIN(1400, vis.size.width), fh = MIN(900, vis.size.height);
	return NSMakeRect(vis.origin.x + (vis.size.width - fw) / 2, vis.origin.y + (vis.size.height - fh) / 2, fw, fh);
}

- (void)buildMenu {
	NSMenu *main = [NSMenu new];

	NSMenuItem *appItem = [NSMenuItem new];
	NSMenu *app = [[NSMenu alloc] initWithTitle:@"HobStack"];
	[app addItem:hsItem(@"About HobStack", @selector(orderFrontStandardAboutPanel:), @"", NSApp)];
	[app addItem:[NSMenuItem separatorItem]];
	[app addItem:hsItem(@"Settings…", @selector(openSettings:), @",", self)];
	[app addItem:[NSMenuItem separatorItem]];
	[app addItem:hsItem(@"Quit HobStack", @selector(quitApp:), @"q", self)];
	appItem.submenu = app;
	[main addItem:appItem];

	NSMenuItem *editItem = [NSMenuItem new];
	NSMenu *edit = [[NSMenu alloc] initWithTitle:@"Edit"];
	[edit addItem:hsItem(@"Undo", NSSelectorFromString(@"undo:"), @"z", nil)];
	NSMenuItem *redo = hsItem(@"Redo", NSSelectorFromString(@"redo:"), @"z", nil);
	redo.keyEquivalentModifierMask = NSEventModifierFlagCommand | NSEventModifierFlagShift;
	[edit addItem:redo];
	[edit addItem:[NSMenuItem separatorItem]];
	[edit addItem:hsItem(@"Cut", @selector(cut:), @"x", nil)];
	[edit addItem:hsItem(@"Copy", @selector(copy:), @"c", nil)];
	[edit addItem:hsItem(@"Paste", @selector(paste:), @"v", nil)];
	[edit addItem:hsItem(@"Select All", @selector(selectAll:), @"a", nil)];
	editItem.submenu = edit;
	[main addItem:editItem];

	NSMenuItem *viewItem = [NSMenuItem new];
	NSMenu *view = [[NSMenu alloc] initWithTitle:@"View"];
	[view addItem:hsItem(@"Reload", @selector(reloadPage:), @"r", self)];
	[view addItem:[NSMenuItem separatorItem]];
	[view addItem:hsItem(@"Back", @selector(goBackPage:), @"[", self)];
	[view addItem:hsItem(@"Forward", @selector(goForwardPage:), @"]", self)];
	[view addItem:[NSMenuItem separatorItem]];
	[view addItem:hsItem(@"Zoom In", @selector(zoomIn:), @"=", self)];
	[view addItem:hsItem(@"Zoom Out", @selector(zoomOut:), @"-", self)];
	[view addItem:hsItem(@"Actual Size", @selector(zoomReset:), @"0", self)];
	viewItem.submenu = view;
	[main addItem:viewItem];

	NSMenuItem *winItem = [NSMenuItem new];
	NSMenu *win = [[NSMenu alloc] initWithTitle:@"Window"];
	[win addItem:hsItem(@"Minimize", @selector(performMiniaturize:), @"m", nil)];
	[win addItem:hsItem(@"Close", @selector(performClose:), @"w", nil)];
	winItem.submenu = win;
	[main addItem:winItem];
	NSApp.windowsMenu = win;

	NSApp.mainMenu = main;
}

- (void)createWithFrame:(NSRect)frame zoom:(double)zoom {
	self.window = [[NSWindow alloc] initWithContentRect:NSMakeRect(0, 0, 800, 600)
		styleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskClosable | NSWindowStyleMaskMiniaturizable | NSWindowStyleMaskResizable
		backing:NSBackingStoreBuffered defer:NO];
	self.window.releasedWhenClosed = NO;
	self.window.title = @"HobStack";
	self.window.minSize = NSMakeSize(480, 360);
	self.window.delegate = self;
	[self.window setFrame:frame display:NO];

	WKWebViewConfiguration *cfg = [WKWebViewConfiguration new];
	WKUserScript *mark = [[WKUserScript alloc] initWithSource:@"window.hobstackApp={platform:\"darwin\"};"
		injectionTime:WKUserScriptInjectionTimeAtDocumentStart forMainFrameOnly:YES];
	[cfg.userContentController addUserScript:mark];
	self.web = [[WKWebView alloc] initWithFrame:self.window.contentView.bounds configuration:cfg];
	self.web.autoresizingMask = NSViewWidthSizable | NSViewHeightSizable;
	self.web.navigationDelegate = self;
	self.web.UIDelegate = self;
	self.web.allowsBackForwardNavigationGestures = YES;
	if (zoom > 0) self.web.pageZoom = zoom;
	if (@available(macOS 13.3, *)) self.web.inspectable = YES; // Safari > Develop, pentru depanare
	[self.web addObserver:self forKeyPath:@"title" options:NSKeyValueObservingOptionNew context:nil];
	[self.window.contentView addSubview:self.web];
	[self buildMenu];
}

- (void)showURL:(NSString *)url frame:(NSRect)frame zoom:(double)zoom {
	if (self.window == nil) {
		[self createWithFrame:frame zoom:zoom];
		[self.web loadRequest:[NSURLRequest requestWithURL:[NSURL URLWithString:url]]];
	}
	[NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
	[NSApp activateIgnoringOtherApps:YES];
	[self.window makeKeyAndOrderFront:nil];
	hsVisible = 1;
}

- (void)observeValueForKeyPath:(NSString *)keyPath ofObject:(id)object change:(NSDictionary *)change context:(void *)context {
	NSString *t = self.web.title;
	self.window.title = t.length ? t : @"HobStack";
}

// inchiderea doar ascunde fereastra: pagina, stream-urile si pozitia in UI raman pentru urmatorul Open
- (BOOL)windowShouldClose:(NSWindow *)sender {
	NSRect f = self.window.frame;
	goWindowClosed(f.origin.x, f.origin.y, f.size.width, f.size.height, self.web.pageZoom);
	[self.window orderOut:nil];
	hsVisible = 0;
	[NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
	return NO;
}

- (void)openSettings:(id)sender {
	[self.web evaluateJavaScript:@"location.hash='#/settings'" completionHandler:nil];
}
- (void)quitApp:(id)sender { goMenuQuit(); }
- (void)reloadPage:(id)sender { [self.web reload]; }
- (void)goBackPage:(id)sender { [self.web goBack]; }
- (void)goForwardPage:(id)sender { [self.web goForward]; }
- (void)zoomIn:(id)sender { self.web.pageZoom = MIN(3.0, self.web.pageZoom + 0.1); }
- (void)zoomOut:(id)sender { self.web.pageZoom = MAX(0.5, self.web.pageZoom - 0.1); }
- (void)zoomReset:(id)sender { self.web.pageZoom = 1.0; }

// navigarile cadrului principal spre alta origine (GitHub, skills.sh, mailto...) merg in browserul de sistem
- (void)webView:(WKWebView *)webView decidePolicyForNavigationAction:(WKNavigationAction *)action
	decisionHandler:(void (^)(WKNavigationActionPolicy))decisionHandler {
	NSURL *u = action.request.URL;
	BOOL mainFrame = action.targetFrame == nil || action.targetFrame.isMainFrame;
	NSString *scheme = u.scheme.lowercaseString;
	BOOL internal = [scheme isEqualToString:@"about"] || [scheme isEqualToString:@"blob"] || [scheme isEqualToString:@"data"] ||
		[hsOriginOf(u) isEqualToString:self.origin];
	if (mainFrame && !internal) {
		[[NSWorkspace sharedWorkspace] openURL:u];
		decisionHandler(WKNavigationActionPolicyCancel);
		return;
	}
	decisionHandler(WKNavigationActionPolicyAllow);
}

// un raspuns pe care fereastra nu-l poate arata (descarcare fara a[download]): il deschide browserul de sistem
- (void)webView:(WKWebView *)webView decidePolicyForNavigationResponse:(WKNavigationResponse *)response
	decisionHandler:(void (^)(WKNavigationResponsePolicy))decisionHandler {
	if (!response.canShowMIMEType && response.isForMainFrame) {
		[[NSWorkspace sharedWorkspace] openURL:response.response.URL];
		decisionHandler(WKNavigationResponsePolicyCancel);
		return;
	}
	decisionHandler(WKNavigationResponsePolicyAllow);
}

// target=_blank / window.open: o singura fereastra, deci se deschide in browserul de sistem
- (WKWebView *)webView:(WKWebView *)webView createWebViewWithConfiguration:(WKWebViewConfiguration *)configuration
	forNavigationAction:(WKNavigationAction *)action windowFeatures:(WKWindowFeatures *)features {
	if (action.request.URL != nil) [[NSWorkspace sharedWorkspace] openURL:action.request.URL];
	return nil;
}

- (NSAlert *)alertWith:(NSString *)message {
	NSAlert *a = [NSAlert new];
	a.messageText = @"HobStack";
	a.informativeText = message;
	return a;
}

- (void)webView:(WKWebView *)webView runJavaScriptAlertPanelWithMessage:(NSString *)message
	initiatedByFrame:(WKFrameInfo *)frame completionHandler:(void (^)(void))completionHandler {
	NSAlert *a = [self alertWith:message];
	[a addButtonWithTitle:@"OK"];
	[a beginSheetModalForWindow:self.window completionHandler:^(NSModalResponse r) { completionHandler(); }];
}

// fara metoda asta confirm() intoarce false fara sa intrebe (Delete, Mark all done, Quit)
- (void)webView:(WKWebView *)webView runJavaScriptConfirmPanelWithMessage:(NSString *)message
	initiatedByFrame:(WKFrameInfo *)frame completionHandler:(void (^)(BOOL))completionHandler {
	NSAlert *a = [self alertWith:message];
	[a addButtonWithTitle:@"OK"];
	[a addButtonWithTitle:@"Cancel"];
	[a beginSheetModalForWindow:self.window completionHandler:^(NSModalResponse r) {
		completionHandler(r == NSAlertFirstButtonReturn);
	}];
}

- (void)webView:(WKWebView *)webView runJavaScriptTextInputPanelWithPrompt:(NSString *)prompt
	defaultText:(NSString *)defaultText initiatedByFrame:(WKFrameInfo *)frame
	completionHandler:(void (^)(NSString *))completionHandler {
	NSAlert *a = [self alertWith:prompt];
	NSTextField *field = [[NSTextField alloc] initWithFrame:NSMakeRect(0, 0, 280, 24)];
	field.stringValue = defaultText ?: @"";
	a.accessoryView = field;
	[a addButtonWithTitle:@"OK"];
	[a addButtonWithTitle:@"Cancel"];
	[a beginSheetModalForWindow:self.window completionHandler:^(NSModalResponse r) {
		completionHandler(r == NSAlertFirstButtonReturn ? field.stringValue : nil);
	}];
}

// <input type=file>: fara metoda asta Attach files / Upload nu fac nimic
- (void)webView:(WKWebView *)webView runOpenPanelWithParameters:(WKOpenPanelParameters *)parameters
	initiatedByFrame:(WKFrameInfo *)frame completionHandler:(void (^)(NSArray<NSURL *> *))completionHandler {
	NSOpenPanel *p = [NSOpenPanel openPanel];
	p.allowsMultipleSelection = parameters.allowsMultipleSelection;
	p.canChooseFiles = YES;
	if (@available(macOS 10.13.4, *)) p.canChooseDirectories = parameters.allowsDirectories;
	[p beginSheetModalForWindow:self.window completionHandler:^(NSModalResponse r) {
		completionHandler(r == NSModalResponseOK ? p.URLs : nil);
	}];
}
@end

// HSAppDelegate: clic pe iconita din Dock / HobStack.app deschis din nou cat ruleaza = arata fereastra;
// Quit din Dock / logout = oprire curata prin Go (NSTerminateLater, raspunsul in hsStopApp).
@interface HSAppDelegate : NSObject <NSApplicationDelegate>
@end
@implementation HSAppDelegate
- (BOOL)applicationShouldHandleReopen:(NSApplication *)sender hasVisibleWindows:(BOOL)flag {
	goWindowReopen();
	return NO;
}
- (NSApplicationTerminateReply)applicationShouldTerminate:(NSApplication *)sender {
	hsTerminating = 1;
	goAppTerminate();
	return NSTerminateLater;
}
@end

static HSWindowCtl *hsCtl;
static HSAppDelegate *hsDelegate; // NSApp.delegate e weak: il tinem noi

// hsInstallAppDelegate: pe firul principal, inainte de [NSApp run]
static void hsInstallAppDelegate(void) {
	[NSApplication sharedApplication];
	hsDelegate = [HSAppDelegate new];
	NSApp.delegate = hsDelegate;
}

static void hsShowWindow(const char *url, const char *origin, double x, double y, double w, double h, double zoom) {
	NSString *u = [NSString stringWithUTF8String:url]; // copiile, inainte de dispatch (C.free dupa apel)
	NSString *o = [NSString stringWithUTF8String:origin];
	dispatch_async(dispatch_get_main_queue(), ^{
		if (hsCtl == nil) hsCtl = [HSWindowCtl new];
		hsCtl.origin = o;
		NSRect frame = [hsCtl initialFrameX:x y:y w:w h:h];
		[hsCtl showURL:u frame:frame zoom:zoom];
	});
}

static int hsWindowVisible(void) { return hsVisible; }

// hsTerminatePending: 1 daca oprirea vine din applicationShouldTerminate (hsStopApp raspunde atunci YES)
int hsTerminatePending(void) { return hsTerminating; }

// hsReplyTerminate: raspunsul la NSTerminateLater (aplicatia iese); pe firul principal.
void hsReplyTerminate(void) {
	dispatch_async(dispatch_get_main_queue(), ^{ [NSApp replyToApplicationShouldTerminate:YES]; });
}
*/
import "C"

import (
	"net/url"
	"sync"
	"unsafe"

	"hobstack/internal/appprefs"
)

// windowHooks: le pune main inainte de primul showWindow.
var windowHooks struct {
	sync.Mutex
	root   string // folderul datelor (app.json: marimea, pozitia, zoom-ul)
	reopen func() // clic pe Dock / HobStack.app deschis din nou
}

func windowAvailable() (bool, string) { return true, "" }

// installAppDelegate: din runMenuBar, pe firul principal, inainte de bucla Cocoa.
func installAppDelegate() { C.hsInstallAppDelegate() }

// showWindow creeaza fereastra (prima data) sau o aduce in fata; sigur din orice goroutine.
func showWindow(rawURL string) {
	windowHooks.Lock()
	root := windowHooks.root
	windowHooks.Unlock()
	var p appprefs.Prefs
	if root != "" {
		p = appprefs.Load(root)
	}
	var x, y, w, h float64
	if p.Frame != nil {
		x, y, w, h = p.Frame.X, p.Frame.Y, p.Frame.W, p.Frame.H
	}
	origin := ""
	if u, err := url.Parse(rawURL); err == nil {
		origin = u.Scheme + "://" + u.Host
	}
	cu, co := C.CString(rawURL), C.CString(origin)
	defer C.free(unsafe.Pointer(cu))
	defer C.free(unsafe.Pointer(co))
	C.hsShowWindow(cu, co, C.double(x), C.double(y), C.double(w), C.double(h), C.double(p.Zoom))
}

// windowOpen: fereastra e pe ecran (pentru restart-ul dupa update: o redeschidem).
func windowOpen() bool { return C.hsWindowVisible() == 1 }
