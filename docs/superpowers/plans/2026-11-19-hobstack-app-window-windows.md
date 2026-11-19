# HobStack app window on Windows — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** On Windows, HobStack opens in its own window (WebView2) by default, like macOS; Browser stays an option.

**Architecture:** A dedicated OS thread (runtime.LockOSThread, COM STA) owns one top-level Win32 window with our own
WndProc and an embedded WebView2 (`github.com/wailsapp/go-webview2/pkg/edge`, pure Go). Close hides the window; Open
shows it again. External links, `target=_blank`, `window.open` and the window title go through a small injected script
(`window_shim.js`) that posts JSON messages to Go (WebMessageReceived), so we need no COM handlers beyond what the
library wires. Server / UI parts (appprefs, /api/app/*, download to Downloads, Settings radio) already exist from the
macOS plan and are reused unchanged.

**Tech Stack:** Go 1.26, golang.org/x/sys/windows via the library, Win32 user32 calls through syscall, WebView2 Runtime.

**Spec:** docs/superpowers/specs/2026-11-18-hobstack-app-window-design.md

## Global Constraints
- Windows release stays `CGO_ENABLED=0`; cross-compile `GOOS=windows go build ./cmd/hobstack` must pass from macOS.
- Linux stays on the browser; the setting does not appear there.
- Window data folder: `<data>/webview2` (not next to the exe).
- First size 1400x900 (scaled by DPI), centred on the work area, limited to it; saved frame used only if at least
  400x300 and visible on a monitor.
- No WebView2 Runtime: `windowAvailable()` = false with reason "WebView2 Runtime is not installed"; HobStack opens the browser.
- User-facing text in English, code comments in Romanian. Minimal dependencies: one new module (+ its go-winloader).

## Rulings vs spec
- Library: `wailsapp/go-webview2` v1.0.23 instead of `jchv/go-webview2`. jchv calls `log.Fatal` on any WebView2 error and
  has no zoom API; wails has an error callback, ZoomFactor and a runtime-version probe. Cost if wrong: swap the import
  (same edge.Chromium shape).
- wails' error callback ends with `os.Exit(1)`. Ours logs, marks the window failed, hides it and parks the window
  thread (`select{}`), so the server keeps running and the next Open uses the browser.
- Navigation / new-window / title via the injected script, not via NavigationStarting / NewWindowRequested COM handlers
  (the library does not wire them). Cost: a navigation started outside a click / window.open (e.g. a form with
  target=_blank) is not caught; HobStack has none.

## Review Focus
1. Window shown from the tray thread / HTTP goroutine / second launch: must come to the foreground with keyboard focus
   (AllowSetForegroundWindow in the second instance; SetForegroundWindow + MoveFocus).
2. Alt+F4 / X / Ctrl+W hide the window; HobStack keeps running in the tray; Open shows the same page again.
3. Saved frame on a monitor that is gone (laptop undocked): window must still appear on screen.
4. WebView2 failure after start (env creation error): HobStack must not exit.
5. Restart after update with the window open: `windowOpen()` must report true so `--show-window` is passed.

---

### Task 1: Cross-platform window logic (testable on macOS)

**Files:**
- Create: `cmd/hobstack/window_logic.go` (no build tag), `cmd/hobstack/window_logic_test.go`
- Modify: `cmd/hobstack/window_darwin.go` (use `clampZoom`)

**Interfaces:**
- Produces: `type winRect struct{ X, Y, W, H int }`;
  `windowRect(saved *appprefs.Frame, work winRect, scale float64, onScreen func(winRect) bool) winRect`;
  `clampZoom(z float64) float64` (0 or NaN -> 1, else clamp to [0.5, 3]);
  `type bridgeMsg struct{ Type, URL, Title string }`;
  `parseBridge(raw, origin string) (action string, arg string)` with action one of `"open"` (external URL to the
  system browser), `"title"`, `""` (ignore).

- [ ] **Step 1: Write the failing tests** `TestWindowRectDefault` (nil saved, work 1920x1040 at 0,0, scale 1 ->
  {260,70,1400,900}); `TestWindowRectScaledAndClamped` (work 1280x680, scale 1.5 -> W=1280,H=680,X=0,Y=0);
  `TestWindowRectSaved` (saved 100,100,800,600, onScreen true -> same); `TestWindowRectSavedOffScreen` (onScreen false
  -> default); `TestWindowRectSavedTooSmall` (300x200 -> default); `TestClampZoom` (0->1, 0.2->0.5, 7->3, 1.25->1.25);
  `TestParseBridge`: open https external -> ("open", url); open same origin -> ""; open `javascript:` / `file:` ->
  ""; mailto -> "open"; title -> ("title", t) with title cut to 200 runes; bad JSON -> "".
- [ ] **Step 2:** `go test ./cmd/hobstack -run 'WindowRect|ClampZoom|ParseBridge'` -> FAIL (undefined).
- [ ] **Step 3:** Implement in `window_logic.go`; darwin `showWindow` passes `clampZoom(p.Zoom)`.
- [ ] **Step 4:** same command -> PASS; `CGO_ENABLED=1 go build ./cmd/hobstack` OK.
- [ ] **Step 5: Commit** "app window: shared frame / zoom / bridge logic".

### Task 2: Windows window

**Files:**
- Create: `cmd/hobstack/window_windows.go`, `cmd/hobstack/window_shim.js` (embedded), `cmd/hobstack/foreground_windows.go`,
  `cmd/hobstack/foreground_other.go`
- Modify: `cmd/hobstack/window_other.go`, `cmd/hobstack/window_other_test.go` (tag `!(darwin && cgo) && !windows`),
  `cmd/hobstack/main.go` (`allowForeground()` before `showRunning`), `go.mod` / `go.sum`

**Interfaces:**
- Consumes: Task 1; existing `windowHooks`, `openBrowser`, `appprefs.Update`.
- Produces (windows): `windowAvailable() (bool, string)` (WebView2 version probe, cached; after a runtime failure
  false + the error); `showWindow(url string)`; `windowOpen() bool`; `allowForeground()` (AllowSetForegroundWindow(ASFW_ANY);
  no-op elsewhere).

Behaviour:
- Thread: started on first `showWindow`; LockOSThread, CoInitializeEx STA (the library does it only for the main
  thread), SetThreadDpiAwarenessContext(PER_MONITOR_AWARE_V2) when available. Class "HobStackWindow", icon = resource 1.
- Cross-thread: `showWindow` stores the URL under a mutex and posts WM_APP+1 to the window (or starts the thread).
- First show: Navigate(url); later shows only ShowWindow(SW_RESTORE if iconic else SW_SHOW) + SetForegroundWindow +
  MoveFocus (same page stays, like macOS).
- WM_CLOSE -> save state, ShowWindow(SW_HIDE). WM_SIZE -> controller bounds = client rect. WM_MOVE ->
  NotifyParentWindowPositionChanged. WM_EXITSIZEMOVE / WM_ACTIVATE(inactive) / hide -> save frame (GetWindowRect,
  skipped when iconic or maximized) and zoom (controller.GetZoomFactor) through appprefs.Update.
- Accelerators (AcceleratorKeyCallback, Ctrl held): W -> hide; Oem comma -> `location.hash='#/settings'`. Rest:
  WebView2 defaults (Ctrl+C/V, F5, Alt+Left, Ctrl+wheel / Ctrl+± zoom).
- Settings: status bar off; script dialogs / file picker are WebView2's own.
- Bridge: `MessageCallback` -> `parseBridge`; "open" -> `openBrowser`; "title" -> SetWindowTextW.
- Shim (`AddScriptToExecuteOnDocumentCreated`): `window.hobstackApp={platform:"windows"}`; capture click/auxclick on
  `a[href]`: same-origin `a[download]` left to app.js; external http(s)/mailto -> preventDefault + post open;
  same-origin with target=_blank / Ctrl / Shift / middle -> preventDefault + `location.href`; `window.open` override
  with the same rules; title posted on DOMContentLoaded and on `<head>` mutations.

- [ ] **Step 1:** `go get github.com/wailsapp/go-webview2@v1.0.23`; write the files.
- [ ] **Step 2:** `GOOS=windows CGO_ENABLED=0 go build ./cmd/hobstack && GOOS=windows go vet ./cmd/hobstack` -> OK;
  `cp window_shim.js x.mjs && node --check x.mjs` -> OK.
- [ ] **Step 3:** `go test ./cmd/hobstack` (macOS) and `CGO_ENABLED=0 go test ./cmd/hobstack` -> PASS;
  `GOOS=linux go build ./cmd/hobstack` OK.
- [ ] **Step 4: Commit** "Windows: native app window (WebView2)".

### Task 3: Docs
- CLAUDE.md "Gata (fereastra proprie ...)" covers Windows; spec gets the rulings above.
- [ ] Commit "docs: app window on Windows".

Manual check (user, on Windows): the macOS checklist (Ctrl+C/V, confirm on Mark all done, Download + Show in
Explorer, Upload, external link, Alt+F4 then tray Open, Settings -> Browser, zoom + restart, Quit from tray), plus a
second launch from the Start menu and a machine without WebView2 Runtime (browser opens).
