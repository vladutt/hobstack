# HobStack app window (macOS) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** On macOS, HobStack opens in its own WKWebView window, the default "App window" mode, with "Browser" still available in Settings > App. Windows and Linux keep the browser until the Windows plan.

**Architecture:** The HTTP server stays as it is. A small pure-Go package keeps the preference and the window frame/zoom in `<data>/app.json`. The web layer gets `/api/app/show`, `/api/app/open-in` and `/api/app/download`. `cmd/hobstack` gets a platform-split window (`window_darwin.go` with Objective-C in the cgo preamble, next to the menu bar; `window_other.go` as a stub everywhere else). `main.go` routes every "open" through one `openUI(url)`.

**Tech Stack:** Go 1.26, cgo + Cocoa + WebKit (WKWebView, macOS 11+), vanilla JS modules.

**Spec:** `docs/superpowers/specs/2026-11-18-hobstack-app-window-design.md`

## Global Constraints

- No new Go dependency in this plan (go-webview2 arrives with the Windows plan).
- The Windows release build is `CGO_ENABLED=0` and must still compile: `GOOS=windows go build ./cmd/hobstack`.
- Linux: browser only. The setting is not shown (`windowSupported: false`).
- Default when `app.json` is missing: `openIn = "window"`.
- `--no-open` (autostart at login, restart after update) never opens anything, except after an update with the window open: there the new flag `--show-window` is added at Reexec.
- First window frame: 1400x900, centered, clamped to the screen.
- Everything the user sees is in English. Code comments are in Romanian.
- Conventions from CLAUDE.md: `go vet ./...` and `go build -race ./...` pass; JS is checked by copying it to `.mjs` and running `node --check`; cross-compile `GOOS=darwin GOARCH=arm64 go build ./cmd/hobstack` and `GOOS=windows go build ./cmd/hobstack`.

## Review Focus

1. A `confirm()` in the window (Delete task, Mark all done, Quit) must show a dialog and return the user's choice. A WKWebView without a UI delegate returns `false` silently. Pinned in Task 4's manual check list.
2. Download of a path that is not `/api/...`, or with a name containing `/` or `..`: refuse (400), and never write outside `~/Downloads`. Pinned in Task 2 tests.
3. Two downloads with the same name: the second becomes `name (2).ext`, never an overwrite. Pinned in Task 2 tests.
4. Corrupt or partial `app.json`: load falls back to the defaults (window mode) and never blocks startup. Pinned in Task 1 tests.
5. Second launch against an older running HobStack that has no `/api/app/show` (404): fall back to openBrowser. Pinned in Task 5 (`showRunning` test with an httptest server).

---

### Task 1: `internal/appprefs` (preference + window frame + zoom)

**Files:**
- Create: `internal/appprefs/appprefs.go`
- Test: `internal/appprefs/appprefs_test.go`

**Interfaces:**
- Produces:
  ```go
  type Frame struct{ X, Y, W, H float64 }          // json: x, y, w, h
  type Prefs struct {
      OpenIn string  `json:"openIn"`               // "window" | "browser"
      Frame  *Frame  `json:"window,omitempty"`
      Zoom   float64 `json:"zoom,omitempty"`       // 0 = 1.0
  }
  const OpenWindow, OpenBrowser = "window", "browser"
  func Load(root string) Prefs                      // never fails: missing/corrupt/unknown openIn -> defaults
  func Save(root string, p Prefs) error             // atomic: temp file + rename, 0644
  func Update(root string, fn func(*Prefs)) error   // Load + fn + Save under a package mutex
  ```

- [ ] **Step 1: Write the failing tests**
  - `TestLoadMissingIsWindow`: `Load(t.TempDir()).OpenIn == "window"`.
  - `TestLoadCorruptIsDefault`: write `{"openIn":` to `app.json`, then `Load(...).OpenIn == "window"`.
  - `TestLoadUnknownOpenIn`: `{"openIn":"tab"}` gives `"window"`.
  - `TestSaveLoadRoundTrip`: save `Prefs{OpenIn:"browser", Frame:&Frame{10,20,1200,800}, Zoom:1.25}`; Load returns the same values.
  - `TestUpdateKeepsOtherFields`: Save with a Frame, then `Update(root, func(p){p.OpenIn="browser"})`; Frame is unchanged.
- [ ] **Step 2: Run `go test ./internal/appprefs/`.** Expected: FAIL (package missing).
- [ ] **Step 3: Implement** the interface above in `appprefs.go`. The file is `filepath.Join(root, "app.json")`.
- [ ] **Step 4: Run `go test -race ./internal/appprefs/`.** Expected: PASS.
- [ ] **Step 5: Commit** `git commit -m "appprefs: open-in preference, window frame and zoom in app.json"`

### Task 2: web API (state, open-in, show, download)

**Files:**
- Modify: `internal/web/api_app.go`, `internal/web/server.go` (routes next to `/api/app`)
- Create: `internal/web/api_app_window.go`
- Test: `internal/web/app_window_test.go` (uses `setup(t)` from `server_test.go`)

**Interfaces:**
- Consumes: `appprefs.Load / Update` (Task 1).
- Produces:
  ```go
  // AppWindow: what main tells the web layer about the window; nil = never set (tests, Linux).
  type AppWindow struct {
      Supported bool
      Reason    string           // why it is not available ("" when Supported)
      Show      func()           // opens the UI as the preference says (window or browser)
  }
  func (s *Server) SetAppWindow(w AppWindow)
  ```
  - Routes:
    - `GET /api/app` also returns `openIn` (from appprefs), `windowSupported` and `windowReason`.
    - `PUT /api/app/open-in` `{openIn:"window"|"browser"}` returns the app state. Any other value: 400. Not supported: 409 `"app window is not available"`.
    - `POST /api/app/show` returns 204 and calls `Show` in a goroutine. With `Show == nil`: 503.
    - `POST /api/app/download` `{url, name}` returns `{path}`.
    - `POST /api/app/reveal` `{path}` returns 204. It only accepts a regular file directly inside `~/Downloads`, otherwise 400, and calls `s.opener.Reveal`.
  - `func uniqueName(dir, name string) string`: `report.pdf`, then `report (2).pdf`, `report (3).pdf`. Without an extension: `notes (2)`.
- Download rules:
  - `url` must start with `/api/` (otherwise 400).
  - `name` is `filepath.Base`. Empty, `.`, `..` or containing a separator: 400.
  - The server runs its own `s.Handler()` with an internal GET request (no network, through a ResponseWriter that writes to a temp file in Downloads). Status != 200: delete the temp file and return 502 with that status. Otherwise rename it to `uniqueName`.
  - Downloads dir is `filepath.Join(s.app.home, "Downloads")`, created if missing. `s.app == nil`: 503.

- [ ] **Step 1: Write the failing tests** (in each test, `e.srv.SetApp(exe, home, root, log, func(){})` with `home := t.TempDir()`):
  - `TestAppStateOpenInDefault`: GET `/api/app` gives `openIn == "window"`, and `windowSupported == false` without SetAppWindow.
  - `TestSetOpenIn`: after `SetAppWindow(AppWindow{Supported:true})`, PUT `browser` gives 200 and GET shows `"browser"`. PUT `"tab"` gives 400.
  - `TestSetOpenInUnsupported`: without SetAppWindow, PUT gives 409.
  - `TestShowCallsHook`: Show sends on a channel; POST gives 204 and the channel receives within 1 s. Without a hook: 503.
  - `TestDownloadSavesFile`: upload a project file the way `files_test.go` does, then POST download `{url:"/api/files/<id>?download=1", name:"a.txt"}`. The response path is `<home>/Downloads/a.txt` with the same content. A second call gives `a (2).txt`.
  - `TestDownloadRejects`: `url:"http://evil/x"` gives 400; `name:"../x"` gives 400; `url:"/api/files/999999"` gives 502 and leaves no file in Downloads.
  - `TestRevealOnlyDownloads`: a path outside Downloads gives 400.
  - `TestUniqueName`: the three cases above.
- [ ] **Step 2: Run `go test ./internal/web/ -run 'App|Download|Reveal|UniqueName'`.** Expected: FAIL.
- [ ] **Step 3: Implement** in `api_app_window.go`, and add the three fields to `appState()`.
- [ ] **Step 4: Run `go test -race ./internal/web/`.** Expected: PASS.
- [ ] **Step 5: Commit** `git commit -m "web: app window state, open-in, show and download to Downloads"`

### Task 3: UI (Settings > App, download in the window, switcher)

**Files:**
- Modify: `internal/web/static/js/app-settings.js`, `internal/web/static/js/app.js`, `internal/web/static/js/project-switcher.js:95`

**Interfaces:**
- Consumes: the Task 2 routes; `window.hobstackApp` (set by Task 4: `{platform:"darwin"}`; missing in a browser).

- [ ] **Step 1: Settings > App.** When `st.windowSupported || st.windowReason`, show a group "Open HobStack in":
  - radio "App window" (hint: "Its own window, like any other app.");
  - radio "Browser" (hint: "A tab in your default browser.").
  - Change: PUT `/api/app/open-in` and redraw from the response. Hint under the group: "Takes effect the next time you open HobStack."
  - With `!windowSupported && windowReason`: radios disabled, and the line `App window is not available: <reason>`.
  - The text "you can close the terminal and this tab" becomes "...and this window" when `window.hobstackApp` exists.
- [ ] **Step 2: Downloads in the window** (`app.js`, only `if (window.hobstackApp)`): a capture-phase click listener on `document` for `a[download]` whose `href` is same-origin `/api/...`.
  - `preventDefault`, then POST `/api/app/download` `{url: pathname + search, name: a.getAttribute("download") || last path segment}`.
  - Success: `toast("Saved to Downloads: <basename>", "ok", {label:"Show in Finder", run: () => api("/api/app/reveal", {method:"POST", body:{path}})})`. Check `ui.js` `toast(message, kind, action)` for the action's exact shape and use that.
  - Error: `toast(err.message)`.
- [ ] **Step 3: Switcher** (`project-switcher.js:95`): with `window.hobstackApp`, `newTab` navigates in the same window (`location.hash = href`) instead of `window.open`.
- [ ] **Step 4: Check the JS.** For each changed file: `cp <f> "$TMPDIR/x.mjs" && node --check "$TMPDIR/x.mjs"`. Expected: no output.
- [ ] **Step 5: Commit** `git commit -m "UI: Open in setting, downloads and switcher in the app window"`

### Task 4: macOS native window

**Files:**
- Create: `cmd/hobstack/window_darwin.go` (`//go:build darwin && cgo`, Objective-C in the preamble, no `//export` in this file)
- Create: `cmd/hobstack/window_exports_darwin.go` (the `//export go...` functions)
- Create: `cmd/hobstack/window_other.go` (`//go:build !(darwin && cgo)`)
- Modify: `installer/macos/Info.plist` (`NSAppTransportSecurity` / `NSAllowsLocalNetworking` = true)

**Interfaces:**
- Produces (same signatures in `window_darwin.go` and `window_other.go`):
  ```go
  func windowAvailable() (bool, string)   // other: false, "not available on this system"
  func showWindow(url string)             // create or bring to front; safe from any goroutine
  func windowOpen() bool                  // a window exists and is visible
  // set once by main, before the first showWindow:
  var windowHooks struct {
      sync.Mutex
      root   string        // data folder, for appprefs (frame, zoom)
      reopen func()        // Dock click / app reopened from Finder while running
  }
  ```
- Consumes: `appprefs.Update` (Task 1); `goMenuQuit` (existing, for Cmd+Q).

What the Objective-C side does (`WebKit` and `Cocoa` frameworks in `#cgo LDFLAGS`, everything on the main queue via `dispatch_async`):
- **Window:**
  - One `NSWindow`, titled / closable / miniaturizable / resizable.
  - Frame from the `x,y,w,h` arguments (passed from appprefs.Load), else 1400x900 centered, clamped to `visibleFrame`.
  - `releasedWhenClosed = NO`.
- **Window delegate:**
  - `windowWillClose`: save the frame and `pageZoom` through `goWindowClosed(x,y,w,h,zoom)`, which does `appprefs.Update`; set the activation policy back to Accessory.
  - `windowDidResize/Move`: nothing (saving on close is enough).
- **Showing:** `NSApplicationActivationPolicyRegular` + `activateIgnoringOtherApps` + `makeKeyAndOrderFront`. If the window exists, only bring it to front: no reload, no URL change.
- **WKWebView:**
  - `WKUserScript` at document start: `window.hobstackApp={platform:"darwin"};`.
  - `allowsBackForwardNavigationGestures = YES`; `pageZoom` from prefs.
  - KVO on `title`: window title (empty gives "HobStack").
- **Navigation delegate:** `decidePolicyForNavigationAction`: a URL whose scheme+host+port differs from the app origin goes to `[[NSWorkspace sharedWorkspace] openURL:]` and is cancelled. Everything else is allowed.
- **UI delegate:**
  - `createWebViewWithConfiguration` (`target=_blank`, `window.open`): open the URL in the system browser and return nil.
  - alert / confirm / prompt panels: `NSAlert` as a sheet on the window. confirm gets OK / Cancel; prompt gets an `NSTextField` accessory.
  - `runOpenPanelWithParameters`: `NSOpenPanel` with `allowsMultipleSelection` from the parameters.
- **Main menu** (set when the window is first created):
  - HobStack: About HobStack (standard panel), Settings… Cmd+, (JS `location.hash='#/settings'`), Quit HobStack Cmd+Q (`goMenuQuit`).
  - Edit: Undo Cmd+Z, Redo Cmd+Shift+Z, Cut, Copy, Paste, Select All (standard selectors, nil target).
  - View: Reload Cmd+R, Back Cmd+[, Forward Cmd+], Zoom In Cmd+=, Zoom Out Cmd+-, Actual Size Cmd+0 (pageZoom ±0.1, clamped to 0.5–3.0).
  - Window: Minimize Cmd+M, Close Cmd+W.
  - No item uses Cmd+K / Cmd+P / Cmd+F / Cmd+Shift+C (the page's own shortcuts).
- **App delegate:** set in `hsRunApp` before `[NSApp run]` (`menubar_darwin.go` gets a small call into the window file, e.g. `hsInstallAppDelegate()`). `applicationShouldHandleReopen:hasVisibleWindows:` calls `goWindowReopen()` and returns NO.

- [ ] **Step 1: Write `window_other.go`.** Then build: `GOOS=windows go build ./cmd/hobstack && GOOS=linux go build ./cmd/hobstack`. Expected: OK.
- [ ] **Step 2: Write `window_darwin.go` + `window_exports_darwin.go`, and add the Info.plist key.** Then `go vet ./cmd/hobstack && go build -race ./cmd/hobstack && GOOS=darwin GOARCH=amd64 CGO_ENABLED=1 CC="clang -arch x86_64" go build -o /dev/null ./cmd/hobstack`. Expected: OK.
- [ ] **Step 3: Commit** `git commit -m "macOS: native app window (WKWebView) with menus, dialogs, file picker and external links"`

(Manual verification of this task happens after Task 5 has wired it in; see Task 6.)

### Task 5: wiring in `main.go`

**Files:**
- Modify: `cmd/hobstack/main.go`
- Test: `cmd/hobstack/main_test.go` (create if missing)

**Interfaces:**
- Consumes: Tasks 1, 2, 4.
- Produces:
  ```go
  func openUI(root, url string)               // window if prefs say so and windowAvailable(); else openBrowser
  func showRunning(addr string) bool          // POST http://addr/api/app/show; true only on 204
  ```

- [ ] **Step 1: Write the failing test.** `TestShowRunning`: an httptest server answering 204 gives true; 404 gives false; a closed port gives false.
- [ ] **Step 2: Run `go test ./cmd/hobstack/ -run ShowRunning`.** Expected: FAIL.
- [ ] **Step 3: Implement and wire:**
  - flag `--show-window` ("open the app window after an update").
  - Both `alreadyRunning` branches: if `!*noOpen`, `if !showRunning(addr) { openBrowser(...) }`.
  - Set `windowHooks.root = paths.Root` and `windowHooks.reopen = func(){ openUI(paths.Root, url) }`.
  - Tray / menu bar open: `openUI(paths.Root, url)` instead of `openBrowser(url)`.
  - Initial open: `if !*noOpen || *showWindow { openUI(...) }`.
  - `web.SetAppWindow(AppWindow{Supported, Reason, Show: func(){ openUI(paths.Root, url) }})` from `windowAvailable()`.
  - Restart after update: `main` appends `--show-window` to the Reexec args when `windowOpen()` was true at shutdown. Capture it in `start` before the server stops, and return it inside `restartError`.
- [ ] **Step 4: Check.** `go test -race ./cmd/hobstack/ && go vet ./... && go build -race ./... && GOOS=windows go build -o /dev/null ./cmd/hobstack && GOOS=darwin GOARCH=arm64 go build -o /dev/null ./cmd/hobstack`. Expected: OK.
- [ ] **Step 5: Commit** `git commit -m "start: open HobStack in the app window by default (macOS)"`

### Task 6: manual check on macOS + docs

**Files:**
- Modify: `CLAUDE.md` (a "Gata (fereastra proprie, macOS ...)" entry pointing to the spec)

- [ ] **Step 1: Run a separate instance.** Build `scripts/build.sh "$SCRATCH/hs"` and start it with `HOME=<scratch home>` on another port (e.g. 7791), so the user's HobStack on 7777 is never touched. Check each, and write down anything that fails:
  1. The window opens at start; the title follows the page; Cmd+C / Cmd+V work in the chat box.
  2. Delete on a task card shows a confirm sheet; Cancel keeps the task, OK deletes it.
  3. Download in Files: the toast "Saved to Downloads", and Show in Finder selects the file.
  4. Attach files opens NSOpenPanel with multiple selection.
  5. The "Release on GitHub" link opens the system browser; the window stays on HobStack.
  6. Cmd+W hides the window and the Dock icon goes away; menu bar "Open HobStack" brings it back with the same page.
  7. A second `hs start --port 7791` brings the window to front and exits 0.
  8. Settings > App: switch to Browser, then Open HobStack opens a browser tab.
  9. Cmd+= then Cmd+W, then reopen: same zoom and same frame.
  10. Cmd+Q with no agents quits; the menu bar icon goes away.
- [ ] **Step 2: Bundle check.** Build `HobStack.app` with `scripts/build-mac.sh` (ad-hoc), open it from Finder, and check items 1, 5 and 7 again. Also check that reopening the .app from Finder while it runs shows the window.
- [ ] **Step 3: Docs + commit.** Add the CLAUDE.md entry, then `git commit -m "Docs: app window on macOS"`.
