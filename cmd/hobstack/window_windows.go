//go:build windows

package main

import (
	_ "embed"
	"errors"
	"log"
	"net/url"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"github.com/wailsapp/go-webview2/pkg/edge"
	"github.com/wailsapp/go-webview2/webviewloader"

	"hobstack/internal/appprefs"
)

// Fereastra proprie pe Windows: o fereastra Win32 cu WebView2 (motorul Edge, instalat pe Windows 10/11), pe un fir al
// ei (runtime.LockOSThread + COM STA) cu bucla ei de mesaje, separat de iconita de langa ceas. Creata la primul Open,
// ascunsa la inchidere (X / Alt+F4 / Ctrl+W), aratata din nou la urmatorul Open, pe aceeasi pagina (ca pe macOS).
// Linkurile externe, target=_blank, window.open si titlul trec prin window_shim.js (mesaje catre Go).

//go:embed window_shim.js
var windowShim string

// windowHooks: le pune main inainte de primul showWindow.
var windowHooks struct {
	sync.Mutex
	root   string // folderul datelor (app.json; WebView2 in <date>/webview2)
	reopen func()
}

var (
	ole32 = syscall.NewLazyDLL("ole32.dll")

	pCoInitializeEx           = ole32.NewProc("CoInitializeEx")
	pShowWindow               = user32.NewProc("ShowWindow")
	pIsWindowVisible          = user32.NewProc("IsWindowVisible")
	pIsIconic                 = user32.NewProc("IsIconic")
	pIsZoomed                 = user32.NewProc("IsZoomed")
	pSetWindowPos             = user32.NewProc("SetWindowPos")
	pGetWindowRect            = user32.NewProc("GetWindowRect")
	pSetWindowText            = user32.NewProc("SetWindowTextW")
	pMonitorFromRect          = user32.NewProc("MonitorFromRect")
	pMonitorFromWindow        = user32.NewProc("MonitorFromWindow")
	pGetMonitorInfo           = user32.NewProc("GetMonitorInfoW")
	pGetDpiForWindow          = user32.NewProc("GetDpiForWindow")              // Windows 10 1607+
	pSetThreadDpiAwarenessCtx = user32.NewProc("SetThreadDpiAwarenessContext") // Windows 10 1607+
	pGetKeyState              = user32.NewProc("GetKeyState")
	pLoadCursor               = user32.NewProc("LoadCursorW")
)

const (
	wmMove          = 0x0003
	wmSize          = 0x0005
	wmSetFocus      = 0x0007
	wmGetMinMaxInfo = 0x0024
	wmExitSizeMove  = 0x0232
	wmShowApp       = wmApp + 2 // showWindow din alt fir: arata fereastra (wmApp + 1 e al iconitei)

	wsOverlappedWindow = 0x00CF0000
	cwUseDefault       = 0x80000000

	swHide    = 0
	swShow    = 5
	swRestore = 9

	swpNoZOrder   = 0x0004
	swpNoActivate = 0x0010

	monitorDefaultToNull    = 0
	monitorDefaultToPrimary = 1

	vkControl  = 0x11
	vkShift    = 0x10
	vkMenu     = 0x12 // Alt
	vkOemComma = 0xBC

	coinitApartmentThreaded = 0x2
	dpiPerMonitorAwareV2    = ^uintptr(3) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 = (HANDLE)-4
)

type rect32 struct{ Left, Top, Right, Bottom int32 }

type monitorInfo struct {
	CbSize    uint32
	RcMonitor rect32
	RcWork    rect32
	DwFlags   uint32
}

// appWindow: o singura fereastra pe proces. Campurile fara mu sunt ale firului ferestrei.
type appWindow struct {
	mu      sync.Mutex
	started bool    // firul ferestrei a pornit
	ready   uintptr // HWND-ul, dupa ce WebView2 e gata (0 pana atunci / dupa o eroare)
	pending string  // adresa ceruta de showWindow (incarcata prima data)
	failed  string  // WebView2 a cazut: de acum se deschide browserul

	hwnd   uintptr
	web    *edge.Chromium
	loaded bool
	origin string
	scale  float64
}

var theWindow appWindow

// probe: WebView2 Runtime instalat (o data per pornire).
var probe struct {
	once   sync.Once
	ok     bool
	reason string
}

func windowAvailable() (bool, string) {
	probe.once.Do(func() {
		v, err := webviewloader.GetAvailableCoreWebView2BrowserVersionString("")
		switch {
		case err != nil:
			probe.reason = "WebView2 Runtime check failed: " + err.Error()
		case v == "":
			probe.reason = "WebView2 Runtime is not installed"
		default:
			probe.ok = true
		}
	})
	theWindow.mu.Lock()
	failed := theWindow.failed
	theWindow.mu.Unlock()
	if failed != "" {
		return false, "WebView2 failed: " + failed
	}
	return probe.ok, probe.reason
}

// showWindow creeaza fereastra (prima data) sau o aduce in fata; sigur din orice goroutine.
func showWindow(rawURL string) {
	w := &theWindow
	w.mu.Lock()
	if w.failed != "" {
		w.mu.Unlock()
		openBrowser(rawURL)
		return
	}
	w.pending = rawURL
	start := !w.started
	w.started = true
	hwnd := w.ready
	w.mu.Unlock()
	switch {
	case start:
		go w.run()
	case hwnd != 0:
		pPostMessage.Call(hwnd, wmShowApp, 0, 0)
	}
	// altfel firul inca porneste: arata fereastra singur cand WebView2 e gata, cu ultima adresa ceruta
}

// windowOpen: fereastra e pe ecran (pentru restart-ul dupa update: o redeschidem).
func windowOpen() bool {
	theWindow.mu.Lock()
	hwnd := theWindow.ready
	theWindow.mu.Unlock()
	if hwnd == 0 {
		return false
	}
	r, _, _ := pIsWindowVisible.Call(hwnd)
	return r != 0
}

func (w *appWindow) run() {
	runtime.LockOSThread() // fereastra, WebView2 si bucla de mesaje raman pe firul asta
	pCoInitializeEx.Call(0, coinitApartmentThreaded)
	if pSetThreadDpiAwarenessCtx.Find() == nil {
		pSetThreadDpiAwarenessCtx.Call(dpiPerMonitorAwareV2) // altfel Windows o scaleaza ca pe o poza: text neclar
	}

	windowHooks.Lock()
	root := windowHooks.root
	windowHooks.Unlock()
	prefs := appprefs.Prefs{}
	if root != "" {
		prefs = appprefs.Load(root)
	}
	w.mu.Lock()
	if u, err := url.Parse(w.pending); err == nil {
		w.origin = u.Scheme + "://" + u.Host
	}
	w.mu.Unlock()

	inst, _, _ := pGetModuleHandle.Call(0)
	class := utf16("HobStackWindow")
	big, _, _ := pLoadImage.Call(inst, 1, 1 /* IMAGE_ICON */, 0, 0, 0x40|0x8000 /* LR_DEFAULTSIZE | LR_SHARED */)
	cx, _, _ := pGetSystemMetrics.Call(49) // SM_CXSMICON
	cy, _, _ := pGetSystemMetrics.Call(50)
	small, _, _ := pLoadImage.Call(inst, 1, 1, cx, cy, 0x8000)
	cursor, _, _ := pLoadCursor.Call(0, 32512) // IDC_ARROW
	wc := wndClassEx{LpfnWndProc: syscall.NewCallback(appWndProc), HInstance: inst, HIcon: big, HIconSm: small,
		HCursor: cursor, LpszClassName: class}
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	if r, _, err := pRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		w.fail(errors.New("RegisterClassEx: " + err.Error()))
		return
	}
	hwnd, _, err := pCreateWindowEx.Call(0, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(utf16("HobStack"))),
		wsOverlappedWindow, cwUseDefault, cwUseDefault, cwUseDefault, cwUseDefault, 0, 0, inst, 0)
	if hwnd == 0 {
		w.fail(errors.New("CreateWindowEx: " + err.Error()))
		return
	}
	w.hwnd = hwnd
	w.scale = dpiScale(hwnd)
	r := windowRect(prefs.Frame, workArea(hwnd), w.scale, onScreen)
	pSetWindowPos.Call(hwnd, 0, uintptr(r.X), uintptr(r.Y), uintptr(r.W), uintptr(r.H), swpNoZOrder|swpNoActivate)

	web := edge.NewChromium()
	if root != "" {
		web.DataPath = filepath.Join(root, "webview2")
	}
	web.SetErrorCallback(w.onError)
	web.MessageCallback = func(msg string, _ *edge.ICoreWebView2, _ *edge.ICoreWebView2WebMessageReceivedEventArgs) {
		w.onMessage(msg)
	}
	web.AcceleratorKeyCallback = w.onKey
	w.web = web
	if !web.Embed(hwnd) {
		w.fail(errors.New("WebView2 could not start"))
		return
	}
	if s, err := web.GetSettings(); err == nil {
		_ = s.PutIsStatusBarEnabled(false) // fara adresa linkului jos, ca intr-un browser
	}
	web.Init(windowShim)
	if ctl := web.GetController(); ctl != nil {
		_ = ctl.PutZoomFactor(clampZoom(prefs.Zoom))
	}
	web.Resize()

	w.mu.Lock()
	w.ready = hwnd
	w.mu.Unlock()
	w.show()

	var m msg
	for {
		r, _, _ := pGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
}

// show: prima data incarca pagina; apoi doar aduce fereastra in fata, pe pagina unde a ramas.
func (w *appWindow) show() {
	w.mu.Lock()
	u := w.pending
	w.mu.Unlock()
	if !w.loaded && u != "" {
		w.web.Navigate(u)
		w.loaded = true
	}
	if r, _, _ := pIsIconic.Call(w.hwnd); r != 0 {
		pShowWindow.Call(w.hwnd, swRestore)
	} else {
		pShowWindow.Call(w.hwnd, swShow)
	}
	pSetForegroundWindow.Call(w.hwnd)
	w.focus()
}

func (w *appWindow) focus() {
	if ctl := w.web.GetController(); ctl != nil {
		_ = ctl.MoveFocus(edge.COREWEBVIEW2_MOVE_FOCUS_REASON_PROGRAMMATIC) // tastele merg direct in pagina
	}
}

// hide: X / Alt+F4 / Ctrl+W. HobStack ramane pornit langa ceas.
func (w *appWindow) hide() {
	w.saveState()
	pShowWindow.Call(w.hwnd, swHide)
}

// saveState: pozitia, marimea (nu cand e minimizata / maximizata) si zoom-ul ajung in app.json.
func (w *appWindow) saveState() {
	windowHooks.Lock()
	root := windowHooks.root
	windowHooks.Unlock()
	if root == "" || w.hwnd == 0 {
		return
	}
	var frame *appprefs.Frame
	iconic, _, _ := pIsIconic.Call(w.hwnd)
	zoomed, _, _ := pIsZoomed.Call(w.hwnd)
	var rc rect32
	if iconic == 0 && zoomed == 0 {
		if r, _, _ := pGetWindowRect.Call(w.hwnd, uintptr(unsafe.Pointer(&rc))); r != 0 {
			frame = &appprefs.Frame{X: float64(rc.Left), Y: float64(rc.Top),
				W: float64(rc.Right - rc.Left), H: float64(rc.Bottom - rc.Top)}
		}
	}
	zoom := 0.0
	if ctl := w.web.GetController(); ctl != nil {
		if z, err := ctl.GetZoomFactor(); err == nil {
			zoom = z
		}
	}
	go func() {
		err := appprefs.Update(root, func(p *appprefs.Prefs) {
			if frame != nil {
				p.Frame = frame
			}
			if zoom > 0 {
				p.Zoom = zoom
			}
		})
		if err != nil {
			log.Printf("app window: saving size: %v", err)
		}
	}()
}

func (w *appWindow) onMessage(raw string) {
	switch action, arg := parseBridge(raw, w.origin); action {
	case "open":
		go openBrowser(arg)
	case "title":
		if arg == "" {
			arg = "HobStack"
		}
		pSetWindowText.Call(w.hwnd, uintptr(unsafe.Pointer(utf16(arg))))
	}
}

// onKey: scurtaturile ferestrei (restul le are WebView2: Ctrl+C / V, F5, Alt+Stanga, zoom cu Ctrl).
func (w *appWindow) onKey(vk uint) bool {
	if !keyDown(vkControl) || keyDown(vkMenu) || keyDown(vkShift) {
		return false
	}
	switch vk {
	case 'W':
		w.hide()
		return true
	case vkOemComma:
		w.web.Eval(`location.hash = "#/settings"`)
		return true
	}
	return false
}

func keyDown(vk uintptr) bool {
	r, _, _ := pGetKeyState.Call(vk)
	return r&0x8000 != 0
}

// onError: biblioteca ar opri tot procesul dupa callback (os.Exit). Nu ne mai intoarcem: ascundem fereastra,
// deschidem pagina in browser si lasam firul ferestrei oprit aici; serverul si agentii merg mai departe.
func (w *appWindow) onError(err error) {
	if err == nil {
		return
	}
	w.fail(err)
	select {}
}

// fail: fereastra nu mai poate fi folosita in pornirea asta; urmatoarele Open merg in browser.
func (w *appWindow) fail(err error) {
	log.Printf("app window: %v (opening the browser instead)", err)
	w.mu.Lock()
	first := w.failed == ""
	if first {
		w.failed = err.Error()
	}
	w.ready = 0
	u := w.pending
	w.mu.Unlock()
	if w.hwnd != 0 {
		pShowWindow.Call(w.hwnd, swHide)
	}
	if first && u != "" {
		go openBrowser(u)
	}
}

func appWndProc(hwnd, message, wparam, lparam uintptr) uintptr {
	w := &theWindow
	if w.web != nil && hwnd == w.hwnd {
		switch message {
		case wmSize:
			w.web.Resize()
		case wmMove:
			if ctl := w.web.GetController(); ctl != nil {
				_ = ctl.NotifyParentWindowPositionChanged()
			}
		case wmSetFocus:
			w.focus()
		case wmExitSizeMove:
			w.saveState()
		case wmClose:
			w.hide()
			return 0
		case wmShowApp:
			w.mu.Lock()
			ready := w.ready != 0
			w.mu.Unlock()
			if ready {
				w.show()
			}
			return 0
		case wmGetMinMaxInfo:
			// MINMAXINFO: ptReserved, ptMaxSize, ptMaxPosition, ptMinTrackSize, ptMaxTrackSize
			pts := *(**[5]point)(unsafe.Pointer(&lparam)) // lparam e adresa structurii (forma pe care go vet o accepta)
			pts[3] = point{int32(float64(minWinW) * w.scale), int32(float64(minWinH) * w.scale)}
			return 0
		}
	}
	r, _, _ := pDefWindowProc.Call(hwnd, message, wparam, lparam)
	return r
}

// workArea: zona de lucru (fara bara de activitati) a monitorului ferestrei.
func workArea(hwnd uintptr) winRect {
	mon, _, _ := pMonitorFromWindow.Call(hwnd, monitorDefaultToPrimary)
	mi := monitorInfo{}
	mi.CbSize = uint32(unsafe.Sizeof(mi))
	if r, _, _ := pGetMonitorInfo.Call(mon, uintptr(unsafe.Pointer(&mi))); r == 0 {
		return winRect{0, 0, defaultWinW, defaultWinH}
	}
	rc := mi.RcWork
	return winRect{int(rc.Left), int(rc.Top), int(rc.Right - rc.Left), int(rc.Bottom - rc.Top)}
}

// onScreen: bara de titlu a marimii salvate e pe un monitor (altfel fereastra n-ar mai putea fi prinsa cu mouse-ul).
func onScreen(r winRect) bool {
	strip := rect32{int32(r.X), int32(r.Y), int32(r.X + r.W), int32(r.Y + 30)}
	mon, _, _ := pMonitorFromRect.Call(uintptr(unsafe.Pointer(&strip)), monitorDefaultToNull)
	return mon != 0
}

func dpiScale(hwnd uintptr) float64 {
	if pGetDpiForWindow.Find() != nil {
		return 1
	}
	dpi, _, _ := pGetDpiForWindow.Call(hwnd)
	if dpi == 0 {
		return 1
	}
	return float64(dpi) / 96
}
