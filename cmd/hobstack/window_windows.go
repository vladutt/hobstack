//go:build windows

package main

import (
	_ "embed"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/wailsapp/go-webview2/pkg/edge"
	"github.com/wailsapp/go-webview2/webviewloader"

	"hobstack/internal/appprefs"
)

// Fereastra proprie pe Windows: o fereastra Win32 cu WebView2 (motorul Edge, instalat pe Windows 10/11), pe un fir al
// ei (runtime.LockOSThread + COM STA) cu bucla ei de mesaje, separat de iconita de langa ceas. Creata la primul Open,
// ascunsa la inchidere (X / Alt+F4 / Ctrl+W), aratata din nou la urmatorul Open, pe aceeasi pagina (ca pe macOS).
// Linkurile externe, target=_blank, window.open si titlul trec prin window_shim.js (mesaje catre Go).
//
// Daca WebView2 nu porneste (eroare sau nimic in 20 s) se mai incearca o data dupa 2 s (dupa un update, procesele
// WebView2 ale versiunii vechi pot tine inca folderul de date), apoi HobStack merge in browser pana la repornire.
// O eroare nu are voie sa opreasca serverul: biblioteca face os.Exit dupa callback-ul de eroare, deci al nostru nu se
// mai intoarce (inchide fereastra si ramane in bucla de mesaje).

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
	wmActivate      = 0x0006
	wmSetFocus      = 0x0007
	wmGetMinMaxInfo = 0x0024
	wmExitSizeMove  = 0x0232
	wmDpiChanged    = 0x02E0
	wmShowApp       = wmApp + 2 // showWindow din alt fir: arata fereastra (wmApp + 1 e al iconitei)
	wmGiveUp        = wmApp + 3 // WebView2 n-a pornit la timp: fereastra se inchide
	wmCloseApp      = wmApp + 4 // inainte de restart (update): fereastra si WebView2 se inchid curat

	sizeMinimized = 1

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
	errClassAlreadyExists   = 1410

	startTimeout = 20 * time.Second
	retryDelay   = 2 * time.Second
)

type rect32 struct{ Left, Top, Right, Bottom int32 }

type monitorInfo struct {
	CbSize    uint32
	RcMonitor rect32
	RcWork    rect32
	DwFlags   uint32
}

// appWindow: starea comuna, sub mu (o singura fereastra pe proces).
type appWindow struct {
	mu      sync.Mutex
	started bool       // prima incercare a pornit
	cur     *winThread // incercarea curenta
	ready   uintptr    // HWND-ul, cat WebView2 merge (0 pana atunci / dupa o eroare)
	pending string     // adresa ceruta de showWindow (incarcata prima data)
	failed  string     // WebView2 a cazut definitiv: pana la repornire se deschide browserul
}

var theWindow appWindow

// winThread: o incercare de a crea fereastra, cu firul ei. Campurile de sus sunt sub theWindow.mu; restul sunt ale
// firului ferestrei (folosite doar de el si de WndProc, care ruleaza tot pe el).
type winThread struct {
	attempt int
	h       uintptr // HWND-ul, pentru celelalte goroutine
	ok      bool    // WebView2 a pornit
	dead    bool    // incercarea s-a terminat (eroare, timp depasit, inchidere)

	hwnd   uintptr
	web    *edge.Chromium
	loaded bool
	origin string
	scale  float64
}

// threads: HWND -> incercarea care l-a creat (pentru WndProc).
var threads sync.Map

// closeDone: inchis de fereastra dupa wmCloseApp.
var closeDone chan struct{}

// probe: WebView2 Runtime instalat (o data per pornire).
var probe struct {
	once   sync.Once
	ok     bool
	reason string
}

func init() {
	clearWebView2Env() // pusa de biblioteca la pornire; agentii n-au ce face cu ea
}

// clearWebView2Env: biblioteca pune WEBVIEW2_* in mediul procesului (ca sa nu le ia din registru); comenzile agentilor
// le-ar mosteni si un agent care lucreaza la o aplicatie WebView2 / Tauri / Wails ar primi alt comportament.
func clearWebView2Env() {
	for _, k := range []string{"WEBVIEW2_PIPE_FOR_SCRIPT_DEBUGGER", "WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS",
		"WEBVIEW2_RELEASE_CHANNEL_PREFERENCE", "WEBVIEW2_BROWSER_EXECUTABLE_FOLDER", "WEBVIEW2_USER_DATA_FOLDER"} {
		os.Unsetenv(k)
	}
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
		startAttempt(0)
	case hwnd != 0:
		pPostMessage.Call(hwnd, wmShowApp, 0, 0)
	}
	// altfel o incercare inca porneste: arata fereastra singura cand WebView2 e gata, cu ultima adresa ceruta
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

// closeWindow: inainte de restart (update) inchide fereastra si WebView2, ca procesul nou sa gaseasca folderul de date
// liber, si ii da voie sa-si aduca fereastra in fata.
func closeWindow() {
	theWindow.mu.Lock()
	hwnd := theWindow.ready
	done := make(chan struct{})
	closeDone = done
	theWindow.mu.Unlock()
	if hwnd == 0 {
		return
	}
	allowForeground()
	pPostMessage.Call(hwnd, wmCloseApp, 0, 0)
	select {
	case <-done:
		time.Sleep(1500 * time.Millisecond) // procesele WebView2 se opresc dupa Close, nu imediat
	case <-time.After(3 * time.Second):
	}
}

func startAttempt(n int) {
	t := &winThread{attempt: n}
	theWindow.mu.Lock()
	theWindow.cur = t
	theWindow.mu.Unlock()
	go t.run()
	time.AfterFunc(startTimeout, func() { t.giveUp(errors.New("WebView2 did not start in 20 s")) })
}

var registerClass = sync.OnceValue(func() error {
	inst, _, _ := pGetModuleHandle.Call(0)
	big, _, _ := pLoadImage.Call(inst, 1, 1 /* IMAGE_ICON */, 0, 0, 0x40|0x8000 /* LR_DEFAULTSIZE | LR_SHARED */)
	cx, _, _ := pGetSystemMetrics.Call(49) // SM_CXSMICON
	cy, _, _ := pGetSystemMetrics.Call(50)
	small, _, _ := pLoadImage.Call(inst, 1, 1, cx, cy, 0x8000)
	cursor, _, _ := pLoadCursor.Call(0, 32512) // IDC_ARROW
	wc := wndClassEx{LpfnWndProc: syscall.NewCallback(appWndProc), HInstance: inst, HIcon: big, HIconSm: small,
		HCursor: cursor, LpszClassName: utf16("HobStackWindow")}
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	if r, _, err := pRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc))); r == 0 && err != syscall.Errno(errClassAlreadyExists) {
		return fmt.Errorf("RegisterClassEx: %v", err)
	}
	return nil
})

func (t *winThread) run() {
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
	theWindow.mu.Lock()
	if u, err := url.Parse(theWindow.pending); err == nil {
		t.origin = u.Scheme + "://" + u.Host
	}
	theWindow.mu.Unlock()

	if err := registerClass(); err != nil {
		t.abort(err, true)
		return
	}
	inst, _, _ := pGetModuleHandle.Call(0)
	hwnd, _, err := pCreateWindowEx.Call(0, uintptr(unsafe.Pointer(utf16("HobStackWindow"))),
		uintptr(unsafe.Pointer(utf16("HobStack"))), wsOverlappedWindow,
		cwUseDefault, cwUseDefault, cwUseDefault, cwUseDefault, 0, 0, inst, 0)
	if hwnd == 0 {
		t.abort(fmt.Errorf("CreateWindowEx: %v", err), true)
		return
	}
	t.hwnd = hwnd
	threads.Store(hwnd, t)
	theWindow.mu.Lock()
	t.h = hwnd
	theWindow.mu.Unlock()
	t.scale = dpiScale(hwnd)
	r := windowRect(prefs.Frame, workArea(hwnd), t.scale, onScreen)
	pSetWindowPos.Call(hwnd, 0, uintptr(r.X), uintptr(r.Y), uintptr(r.W), uintptr(r.H), swpNoZOrder|swpNoActivate)

	web := edge.NewChromium()
	if root != "" {
		web.DataPath = filepath.Join(root, "webview2")
	}
	web.SetErrorCallback(t.onError)
	web.MessageCallback = func(msg string, _ *edge.ICoreWebView2, _ *edge.ICoreWebView2WebMessageReceivedEventArgs) {
		t.onMessage(msg)
	}
	web.AcceleratorKeyCallback = t.onKey
	web.ProcessFailedCallback = t.onProcessFailed
	t.web = web
	web.Embed(hwnd) // asteapta mediul si controller-ul (pompeaza mesaje); erorile ajung in onError
	clearWebView2Env()

	theWindow.mu.Lock()
	dead := t.dead // timp depasit cat astepta: giveUp a hotarat deja ce urmeaza
	theWindow.mu.Unlock()
	if dead {
		if ctl := web.GetController(); ctl != nil {
			closeController(ctl) // a pornit totusi, prea tarziu: wmGiveUp a inchis deja fereastra
		}
		t.destroy()
		return
	}
	if s, err := web.GetSettings(); err == nil {
		_ = s.PutIsStatusBarEnabled(false) // fara adresa linkului jos, ca intr-un browser
	}
	web.Init(windowShim)
	if ctl := web.GetController(); ctl != nil {
		_ = ctl.PutZoomFactor(clampZoom(prefs.Zoom))
		if c3 := ctl.GetICoreWebView2Controller3(); c3 != nil {
			_ = c3.PutRasterizationScale(t.scale) // biblioteca opreste detectarea scalei: o dam noi (si la WM_DPICHANGED)
		}
	}
	web.Resize()

	theWindow.mu.Lock()
	t.ok = true
	theWindow.ready = hwnd
	theWindow.mu.Unlock()
	t.show()
	t.pump()
}

// pump: bucla de mesaje a firului ferestrei.
func (t *winThread) pump() {
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
func (t *winThread) show() {
	theWindow.mu.Lock()
	u := theWindow.pending
	theWindow.mu.Unlock()
	if !t.loaded && u != "" {
		t.web.Navigate(u)
		t.loaded = true
	}
	if ctl := t.web.GetController(); ctl != nil {
		_ = ctl.PutIsVisible(true)
	}
	if r, _, _ := pIsIconic.Call(t.hwnd); r != 0 {
		pShowWindow.Call(t.hwnd, swRestore)
	} else {
		pShowWindow.Call(t.hwnd, swShow)
	}
	pSetForegroundWindow.Call(t.hwnd)
	t.focus()
}

func (t *winThread) focus() {
	if ctl := t.web.GetController(); ctl != nil {
		_ = ctl.MoveFocus(edge.COREWEBVIEW2_MOVE_FOCUS_REASON_PROGRAMMATIC) // tastele merg direct in pagina
	}
}

// hide: X / Alt+F4 / Ctrl+W. HobStack ramane pornit langa ceas; pagina ascunsa nu mai deseneaza.
func (t *winThread) hide() {
	t.saveState()
	if ctl := t.web.GetController(); ctl != nil {
		_ = ctl.PutIsVisible(false)
	}
	pShowWindow.Call(t.hwnd, swHide)
}

// saveState: pozitia, marimea (nu cand e minimizata / maximizata) si zoom-ul ajung in app.json. Pe loc (fisierul e
// mic): doua salvari la rand nu se pot inversa.
func (t *winThread) saveState() {
	windowHooks.Lock()
	root := windowHooks.root
	windowHooks.Unlock()
	if root == "" || t.web == nil {
		return
	}
	var frame *appprefs.Frame
	iconic, _, _ := pIsIconic.Call(t.hwnd)
	zoomed, _, _ := pIsZoomed.Call(t.hwnd)
	var rc rect32
	if iconic == 0 && zoomed == 0 {
		if r, _, _ := pGetWindowRect.Call(t.hwnd, uintptr(unsafe.Pointer(&rc))); r != 0 {
			frame = &appprefs.Frame{X: float64(rc.Left), Y: float64(rc.Top),
				W: float64(rc.Right - rc.Left), H: float64(rc.Bottom - rc.Top)}
		}
	}
	zoom := 0.0
	if ctl := t.web.GetController(); ctl != nil {
		if z, err := ctl.GetZoomFactor(); err == nil {
			zoom = z
		}
	}
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
}

func (t *winThread) onMessage(raw string) {
	switch action, arg := parseBridge(raw, t.origin); action {
	case "open":
		go openBrowser(arg)
	case "title":
		if arg == "" {
			arg = "HobStack"
		}
		pSetWindowText.Call(t.hwnd, uintptr(unsafe.Pointer(utf16(arg))))
	}
}

// onKey: scurtaturile ferestrei (restul le are WebView2: Ctrl+C / V, F5, Alt+Stanga, zoom cu Ctrl). Biblioteca
// marcheaza tasta ca netratata oricum, deci ajunge si in pagina; pagina nu foloseste Ctrl+W / Ctrl+,.
func (t *winThread) onKey(vk uint) bool {
	if !keyDown(vkControl) || keyDown(vkMenu) || keyDown(vkShift) {
		return false
	}
	switch vk {
	case 'W':
		t.hide()
		return true
	case vkOemComma:
		t.web.Eval(`location.hash = "#/settings"`)
		return true
	}
	return false
}

func keyDown(vk uintptr) bool {
	r, _, _ := pGetKeyState.Call(vk)
	return r&0x8000 != 0
}

// onProcessFailed: procesul paginii a cazut = pagina se incarca din nou; procesul browserului WebView2 a cazut =
// fereastra nu mai merge (browserul de sistem, daca era pe ecran).
func (t *winThread) onProcessFailed(_ *edge.ICoreWebView2, args *edge.ICoreWebView2ProcessFailedEventArgs) {
	kind, err := args.GetProcessFailedKind()
	if err != nil {
		return
	}
	switch kind {
	case edge.COREWEBVIEW2_PROCESS_FAILED_KIND_BROWSER_PROCESS_EXITED:
		t.abort(errors.New("the WebView2 browser process exited"), false)
	case edge.COREWEBVIEW2_PROCESS_FAILED_KIND_RENDER_PROCESS_EXITED:
		theWindow.mu.Lock()
		u := theWindow.pending
		theWindow.mu.Unlock()
		if u != "" {
			t.web.Navigate(u)
		}
	}
}

// onError: biblioteca ar opri tot procesul dupa callback (os.Exit), deci nu ne mai intoarcem: inchidem fereastra si
// ramanem in bucla de mesaje a firului (fara ea mesajele trimise ferestrelor firului ar astepta la nesfarsit).
func (t *winThread) onError(err error) {
	if err == nil {
		return
	}
	t.abort(err, false)
	t.pump()
	select {}
}

// abort: pe firul ferestrei. Inchide fereastra si hotaraste ce urmeaza (o noua incercare sau browserul).
func (t *winThread) abort(err error, startup bool) {
	theWindow.mu.Lock()
	if t.dead {
		theWindow.mu.Unlock()
		return
	}
	t.dead = true
	ok := t.ok
	theWindow.mu.Unlock()
	visible := false
	if t.hwnd != 0 {
		r, _, _ := pIsWindowVisible.Call(t.hwnd)
		visible = r != 0
	}
	t.destroy()
	t.after(err, !ok || startup || visible)
}

// giveUp: din timer, daca WebView2 n-a pornit in startTimeout.
func (t *winThread) giveUp(err error) {
	theWindow.mu.Lock()
	if t.ok || t.dead {
		theWindow.mu.Unlock()
		return
	}
	t.dead = true
	hwnd := t.h
	theWindow.mu.Unlock()
	if hwnd != 0 {
		pPostMessage.Call(hwnd, wmGiveUp, 0, 0) // firul e inca in Embed, care pompeaza mesaje
	}
	t.after(err, true)
}

// after: incercarea t s-a terminat cu err. Prima incercare, inainte sa porneasca: inca una dupa 2 s. Altfel browserul
// pana la repornire (si pagina in browser acum, daca utilizatorul o astepta).
func (t *winThread) after(err error, openNow bool) {
	theWindow.mu.Lock()
	if theWindow.cur != t {
		theWindow.mu.Unlock()
		return
	}
	theWindow.ready = 0
	retry := !t.ok && t.attempt == 0
	if !retry {
		theWindow.failed = err.Error()
	}
	u := theWindow.pending
	theWindow.mu.Unlock()
	if retry {
		log.Printf("app window: %v; trying again", err)
		time.AfterFunc(retryDelay, func() { startAttempt(1) })
		return
	}
	log.Printf("app window: %v (opening the browser instead)", err)
	if openNow && u != "" {
		go openBrowser(u)
	}
}

// destroy: pe firul ferestrei. Inchide WebView2 (procesele lui se opresc) si fereastra.
func (t *winThread) destroy() {
	if t.web != nil {
		if ctl := t.web.GetController(); ctl != nil {
			closeController(ctl)
		}
		t.web = nil
	}
	if t.hwnd != 0 {
		threads.Delete(t.hwnd)
		pDestroyWindow.Call(t.hwnd)
		t.hwnd = 0
	}
}

// closeController: ICoreWebView2Controller::Close (biblioteca n-o expune). Locul 24 in vtable: 3 din IUnknown,
// apoi metodele controller-ului in ordinea din WebView2.h (ultima inainte de get_CoreWebView2).
func closeController(ctl *edge.ICoreWebView2Controller) {
	vtbl := *(**[25]uintptr)(unsafe.Pointer(ctl))
	syscall.SyscallN(vtbl[24], uintptr(unsafe.Pointer(ctl)))
}

func appWndProc(hwnd, message, wparam, lparam uintptr) uintptr {
	v, _ := threads.Load(hwnd)
	t, _ := v.(*winThread)
	if t != nil && t.web != nil {
		switch message {
		case wmSize:
			if ctl := t.web.GetController(); ctl != nil {
				vis, _, _ := pIsWindowVisible.Call(hwnd)
				_ = ctl.PutIsVisible(wparam != sizeMinimized && vis != 0)
			}
			t.web.Resize()
		case wmMove:
			if ctl := t.web.GetController(); ctl != nil {
				_ = ctl.NotifyParentWindowPositionChanged()
			}
		case wmSetFocus:
			t.focus()
		case wmActivate:
			if wparam&0xffff == 0 { // WA_INACTIVE: utilizatorul a trecut in alta parte (de ex. Quit din iconita)
				t.saveState()
			}
		case wmExitSizeMove:
			t.saveState()
		case wmClose:
			t.hide()
			return 0
		case wmShowApp:
			theWindow.mu.Lock()
			ok := t.ok && !t.dead
			theWindow.mu.Unlock()
			if ok {
				t.show()
			}
			return 0
		case wmCloseApp:
			t.saveState()
			theWindow.mu.Lock()
			t.dead = true
			theWindow.ready = 0
			done := closeDone
			theWindow.mu.Unlock()
			t.destroy()
			if done != nil {
				close(done)
			}
			return 0
		case wmDpiChanged:
			// fereastra a trecut pe un monitor cu alta scala: marimea sugerata de Windows si scala paginii
			t.scale = float64(wparam>>16&0xffff) / 96
			rc := *(**rect32)(unsafe.Pointer(&lparam))
			pSetWindowPos.Call(hwnd, 0, uintptr(rc.Left), uintptr(rc.Top), uintptr(rc.Right-rc.Left),
				uintptr(rc.Bottom-rc.Top), swpNoZOrder|swpNoActivate)
			if ctl := t.web.GetController(); ctl != nil {
				if c3 := ctl.GetICoreWebView2Controller3(); c3 != nil {
					_ = c3.PutRasterizationScale(t.scale)
				}
			}
			return 0
		case wmGetMinMaxInfo:
			// MINMAXINFO: ptReserved, ptMaxSize, ptMaxPosition, ptMinTrackSize, ptMaxTrackSize
			pts := *(**[5]point)(unsafe.Pointer(&lparam)) // lparam e adresa structurii (forma pe care go vet o accepta)
			pts[3] = point{int32(float64(minWinW) * t.scale), int32(float64(minWinH) * t.scale)}
			return 0
		}
	}
	if t != nil && message == wmGiveUp {
		t.destroy()
		return 0
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
