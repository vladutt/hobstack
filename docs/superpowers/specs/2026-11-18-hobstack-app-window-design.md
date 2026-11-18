# HobStack: fereastra proprie, fara browser

Data: 2026-11-18
Status: cerut de utilizator: "am putea face hobstack un program in sine, fara browser?", apoi "hai sa facem asta cu
programul, sa scapam de browser. Default vine cu open in app window, doar linux este blocat pe browser". Ales:
straturi subtiri per platforma (varianta 1), o singura fereastra.

## De ce

HobStack ruleaza deja ca aplicatie (bara de meniu pe macOS, iconita langa ceas pe Windows, pornire la login), dar UI-ul
se deschide intr-un tab de browser, amestecat cu restul tab-urilor. Utilizatorul vrea un program in sine: clic pe
iconita = fereastra HobStack.

## Scop

- macOS si Windows: HobStack se deschide intr-o fereastra nativa care afiseaza UI-ul de acum (acelasi HTML / JS de pe
  127.0.0.1:<port>), prin motorul web al sistemului: WKWebView pe macOS, WebView2 pe Windows.
- Implicit "App window". In Settings > App se poate alege "Browser" (comportamentul de azi).
- Linux: ramane pe browser, fara alegere (setarea nu apare).

## Non-scop

- Fara UI nativ rescris; fara Electron / Wails / webview_go.
- Fara mai multe ferestre si fara tab-uri in fereastra: o singura fereastra; Cmd/Ctrl+K sare intre proiecte.
- Fara fereastra pe Linux (WebKitGTK ar cere cgo si biblioteci instalate).
- Server-ul HTTP ramane exact cum e: agentii au nevoie de el (MCP pe /mcp/<token>), iar "Browser" il foloseste direct.

## Anti-scop (ar fi un esec chiar daca merge)

- Actiuni care merg in browser dar nu in fereastra: un confirm() care intoarce false fara sa intrebe (Delete, Mark all
  done, Quit), Download care nu face nimic, Cmd+C / Cmd+V care nu merg, un link extern care inlocuieste UI-ul in
  fereastra fara drum inapoi.
- Build-ul de Windows care cere cgo (release-ul e CGO_ENABLED=0, cross-compile din CI).
- Fereastra care apare la pornirea la login (autostart trebuie sa ramana tacut).

## Abordari luate in calcul

1. **Straturi subtiri per platforma (ales).** macOS: WKWebView scris in Objective-C in preambulul cgo de langa bara de
   meniu (menubar_darwin.go are deja Cocoa, firul principal si [NSApp run]); fara biblioteca noua. Windows:
   github.com/jchv/go-webview2 (Go pur, fara cgo; incarca WebView2Loader din memorie). O singura dependenta noua,
   build-urile raman cum sunt.
2. **webview/webview_go**: o biblioteca pentru toate platformele, dar cere cgo si pe Windows: ar strica build-ul de
   release de azi. Respins.
3. **Wails**: framework complet (bundling, bridge, build tool); ar muta structura proiectului pentru putin castig,
   pentru ca tray-ul, bundle-ul .app, installerul si update-ul le avem deja. Respins.

## Cum se comporta

### Pornire si deschidere
- `start` fara `--no-open`, cu modul App window: dupa ce serverul asculta, deschide fereastra pe URL-ul real (in loc de
  openBrowser). Cu modul Browser: openBrowser, ca azi.
- `--no-open` (autostart la login, restart dupa update) nu deschide nimic. Exceptie: restart dupa update cu fereastra
  deschisa: fereastra se redeschide (flag nou, de ex. `--show-window`, adaugat de main la Reexec doar daca fereastra era
  deschisa).
- "Open HobStack" din bara de meniu / clic pe iconita Windows: arata fereastra (o creeaza daca e inchisa, o aduce in
  fata daca exista). In modul Browser: openBrowser.
- Pornit a doua oara (alreadyRunning): cere instantei care ruleaza sa-si arate fereastra (endpoint nou POST
  /api/app/show, doar de pe localhost ca tot API-ul) si iese cu 0. Daca instanta veche nu stie endpoint-ul (404) sau e in
  modul Browser, openBrowser ca azi.

### Fereastra
- Titlul ferestrei urmeaza document.title ("<Proiect> · <Sectiune> — HobStack").
- Marimea si pozitia se tin minte (macOS: setFrameAutosaveName; Windows: in fisierul de setari, vezi mai jos). Prima
  data: 1400x900, centrata, limitata la ecran.
- Inchiderea ferestrei (rosu / X / Cmd+W) doar o ascunde sau o distruge; HobStack ramane pornit in bara de meniu / langa
  ceas, ca azi cand inchizi tab-ul.
- macOS: cat e fereastra deschisa aplicatia are iconita in Dock si meniu (NSApplicationActivationPolicyRegular); la
  inchidere revine la Accessory (doar bara de meniu). Clic pe iconita din Dock = arata fereastra.
- macOS, meniul aplicatiei: HobStack (About, Settings… = #/settings, Quit HobStack Cmd+Q), Edit (Undo, Redo, Cut, Copy,
  Paste, Select All: fara ele Cmd+C / Cmd+V nu merg in WKWebView), View (Reload Cmd+R), Window (Minimize, Close Cmd+W).
  Cmd+Q = acelasi drum ca "Quit HobStack" din bara de meniu (cu confirmarea cand lucreaza agenti). Restul scurtaturilor
  (Cmd+K, Cmd+P, Cmd+F, Cmd+Shift+C...) ajung la pagina: meniul nu le revendica.
- macOS, inapoi / zoom (decizia mea: ce dadea browserul gratis): View are si Back (Cmd+[), Forward (Cmd+]), Zoom In
  (Cmd+=), Zoom Out (Cmd+-), Actual Size (Cmd+0); allowsBackForwardNavigationGestures (swipe) pornit. Nivelul de zoom se
  tine in app.json langa marimea ferestrei. Windows: WebView2 are deja Alt+Stanga / Ctrl+zoom; zoom-ul se tine la fel.
- macOS, HobStack.app deschis din Finder / Spotlight / Dock cat ruleaza deja: LaunchServices trimite reopen
  (applicationShouldHandleReopen:), nu trece prin alreadyRunning: arata fereastra (in modul Browser: openBrowser).
  Pornirea la login nu e afectata (LaunchAgent-ul ruleaza binarul cu start --no-open, fara reopen).
- Windows: Ctrl+C / Ctrl+V merg din WebView2; Alt+F4 inchide fereastra (HobStack ramane langa ceas).

### Ce trebuie facut explicit in fereastra
- **confirm() / alert() / prompt()**: macOS: WKUIDelegate (runJavaScriptConfirmPanel... / Alert... / TextInput...) cu
  NSAlert ca foaie pe fereastra. Windows: WebView2 le arata singur (de verificat).
- **Download** (a[download], ?download=1): in fereastra, UI-ul intercepteaza clicul (listener global in app.js, activ
  doar in fereastra) si cere serverului sa salveze fisierul in folderul Downloads al utilizatorului (endpoint nou, de ex.
  POST /api/app/download {url, name}; serverul isi cheama propriul handler intern, nu face request pe retea; nume unic
  "nume (2).ext"); toast "Saved to Downloads" cu "Show in Finder / Explorer" (opener.Reveal existent). Acelasi drum pe
  ambele platforme, fara API-uri native de download. Doar cai interne (/api/...).
- **Alegerea fisierelor** (<input type=file>: Attach files, Upload, proiect nou, Brainstorm): macOS:
  runOpenPanelWithParameters cu NSOpenPanel (multiple dupa parametri); fara el Upload nu face nimic. Windows: WebView2
  il are singur.
- **Linkuri externe** (orice origine alta decat 127.0.0.1:<port>: GitHub, skills.sh, linkuri din markdown): se deschid
  in browserul de sistem, fereastra ramane pe loc. macOS: decidePolicyForNavigationAction; Windows: NavigationStarting
  / NewWindowRequested.
- **target=_blank si window.open catre pagini interne** (fisiere /api/files/..., brainstorm, Cmd+Enter din switcher):
  - Cmd+Enter din switcher (newTab): in fereastra navigheaza in aceeasi fereastra (o singura fereastra).
  - Linkurile _blank catre fisiere: in browserul de sistem (rar; pozele au deja image-viewer, Files are Open / Download).
- **Cum stie UI-ul ca e in fereastra**: fereastra injecteaza la crearea fiecarui document `window.hobstackApp =
  {platform: "darwin"|"windows"}` (WKUserScript / AddScriptToExecuteOnDocumentCreated). In browser lipseste.

### Setarea "Open in"
- Settings > App, doar pe macOS / Windows: "Open HobStack in: App window / Browser" (radio). Implicit App window.
- Se tine intr-un fisier in folderul datelor (ca marcajul autostart-chosen), de ex. `<date>/app.json`
  `{"openIn": "window"|"browser", "window": {x, y, w, h}}`; lipsa fisierului = App window. GET /api/app intoarce si
  openIn + windowSupported; PUT /api/app/open-in il schimba. Schimbarea are efect la urmatoarea deschidere (fereastra
  deschisa nu se inchide singura).
- Daca fereastra nu poate fi creata (Windows fara WebView2 Runtime, rar pe 10/11): log + openBrowser, o data per
  pornire; Settings arata "App window is not available: <motiv>" si radio-ul e dezactivat.

### Date ale paginii
- localStorage / sessionStorage traiesc in profilul motorului web, separat de browser: preferintele de UI (drawer,
  proiecte recente, categoria aleasa...) pornesc de la zero in fereastra. Acceptat.
- Windows: folderul de date WebView2 in `<date>/webview2` (nu langa exe: instalat in Program Files nu se poate scrie).
- macOS: magazinul implicit al WKWebView (persistent, pe bundle id-ul aplicatiei). Pornit fara bundle (go run / binarul
  din tar.gz) merge si el, cu magazinul procesului.

## Unde sta codul (propunere, builder-ul poate schimba)
- macOS: cmd/hobstack/window_darwin.go (+ exporturile in fisierul de exporturi), langa menubar_darwin.go; acelasi fir
  principal si aceeasi bucla [NSApp run].
- Windows: cmd/hobstack/window_windows.go cu go-webview2, pe un fir al lui (runtime.LockOSThread) cu bucla lui de
  mesaje, ca tray_windows.go. Creat la cerere, distrus la inchidere, recreat la urmatorul Open.
- Linux / altele: window_other.go cu windowAvailable() = false.
- Setarile: un mic pachet (de ex. internal/appprefs) sau in config; endpoint-urile in internal/web langa /api/app.
- UI: app-settings.js (radio-ul), app.js (download, _blank, newTab in fereastra).

## Verificare
- `go vet`, `go build -race`, cross-compile darwin/arm64 si windows (CGO_ENABLED=0), testele existente.
- Manual pe macOS (build cu cgo, si din HobStack.app): deschidere din bara de meniu, Dock, a doua pornire, Cmd+Q cu agenti
  activi, confirm() pe Delete task, Download din Files, link GitHub din Settings > Version, Cmd+C / Cmd+V in chat,
  titlul ferestrei, restart dupa update cu fereastra deschisa, autostart fara fereastra.
- Windows: aceeasi lista intr-o masina Windows (sau VM), inclusiv fara WebView2 Runtime -> browser.

## Note pentru builder (din verificare)
- macOS: de verificat ca WKWebView incarca http://127.0.0.1 din HobStack.app; daca App Transport Security il opreste,
  NSAllowsLocalNetworking in installer/macos/Info.plist. Si din binarul fara bundle.
- Windows: daca go-webview2 nu permite ascunderea ferestrei (Run se termina la inchidere), fereastra se distruge si se
  recreeaza la urmatorul Open (deja permis mai sus).

## Lasat builder-ului
- API-ul exact al go-webview2 si versiunea (fixata, mai veche de 2 saptamani), structura fisierelor, numele
  endpoint-urilor, cum se cheama intern handler-ul pentru download, detaliile de threading.
