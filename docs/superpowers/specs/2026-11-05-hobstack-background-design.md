# HobStack: iconita, rulare fara terminal, pornire la login

Data: 2026-11-05
Status: cerut de utilizator: "iconita la exe cu acel H galben", "porneste automat la restart?", "nu as vrea sa stam
cu terminalul deschis ca sa mearga".

## Iconita

- assets/icon.png (512) si assets/hobstack.ico (16-256), randate din logo-ul din UI (patrat amber #f2b544, colturi
  9/32, "H" Figtree 700 #2a1d05). favicon.png (64) in UI.
- cmd/hobstack/rsrc_windows_amd64.syso: RT_ICON + RT_GROUP_ICON, scris de scripts/winres (doar biblioteca standard,
  obiect COFF cu relocari ADDR32NB fata de .rsrc); go build il ia singur pentru windows/amd64. Refacut cu
  `go run ./scripts/winres assets/hobstack.ico cmd/hobstack/rsrc_windows_amd64.syso`.

## Fara terminal

- Windows: binarul ramane aplicatie de consola, dar cand consola e doar a lui (dublu-clic, pornire la login;
  GetConsoleProcessList == 1) o ascunde (ShowWindow SW_HIDE) si scrie log-ul in ~/.hobstack/hobstack.log (peste
  10 MB -> .1). Nu -H=windowsgui: fara consola, fiecare git / claude / powershell pornit de agenti si-ar deschide
  fereastra lui; asa o mostenesc pe cea ascunsa. Erorile de pornire apar intr-un MessageBox.
- Pornit a doua oara (port ocupat de un HobStack: /api/info raspunde cu numele): deschide browserul pe instanta
  care ruleaza si iese cu 0.
- macOS: dublu-clic in Finder tot deschide Terminal (ar trebui un .app); pornirea la login nu.

## Iconita langa ceas (Windows)

- cmd/hobstack/tray_windows.go: Shell_NotifyIconW cu o fereastra ascunsa si bucla de mesaje pe un fir blocat
  (runtime.LockOSThread). Iconita = grupul 1 din resursele exe-ului, la SM_CXSMICON. Clic / dublu-clic = deschide
  HobStack in browser; clic dreapta = meniul Open HobStack / Quit HobStack (cu agenti care lucreaza: MessageBox
  Yes/No inainte). TaskbarCreated (Explorer repornit) o pune din nou. Close (defer in start) o scoate la oprire si
  inainte de restart-ul dupa update. Marimile structurilor (976 / 80 / 48 pe x64) sunt verificate la compilare.
- macOS / Linux: fara (ar cere Cocoa / un tray Linux); se deschide din browser si se opreste din Settings.

## Pornire la login (internal/autostart), Settings > App

- Windows: HKCU\Software\Microsoft\Windows\CurrentVersion\Run, valoarea "HobStack" = `"<exe>" start --no-open` (reg.exe).
- macOS: ~/Library/LaunchAgents/app.hobstack.plist (RunAtLoad, log-ul in ~/.hobstack/hobstack.log, PATH-ul procesului
  de acum plus ~/.local/bin, /opt/homebrew/bin...: launchd porneste cu un PATH minim, iar agentii au nevoie de tool-uri).
- Linux: ~/.config/autostart/hobstack.desktop (XDG).
- Calea binarului ramane aceeasi dupa update (swap in loc).
- Implicit pornita (cerut: "sa fie bifat automat"): o versiune de release (X.Y.Z, nu dev / git describe) o activeaza
  la prima pornire (autostart.Default) si scrie marcajul ~/.hobstack/autostart-chosen; schimbarea din Settings scrie si
  ea marcajul, deci o debifare ramane. Build-urile locale nu o activeaza singure.
- Quit HobStack: POST /api/app/quit; cu agenti care ruleaza cere confirmare (force), task-urile revin in coada la
  urmatoarea pornire. API: GET /api/app, PUT /api/app/autostart.

## Installer Windows (cerut: "mini install: sa aleaga unde merge .exe-ul, langa exe log-urile si restul, iconita pe desktop")

- installer/hobstack.nsi (NSIS 3, MUI2), construit de scripts/build-release.sh cu makensis (in CI din apt, obligatoriu
  prin REQUIRE_INSTALLER=1; local doar daca exista): HobStack-Setup-<ver>.exe in release si in checksums.txt.
- Pentru utilizatorul curent, fara admin: implicit %LOCALAPPDATA%\Programs\HobStack, folderul se alege. Pagini:
  Welcome, Directory, Components (Desktop shortcut, Start menu shortcut, bifate), Install, Finish ("Start HobStack").
- Inainte sa copieze, exe-ul nou (din $PLUGINSDIR) ruleaza `hobstack quit`: opreste HobStack-ul de pe port, oricare
  ar fi (si o versiune veche pornita din zip); cod 2 = lucreaza agenti -> intreaba, apoi `quit --force`.
- Scrie hobstack.portable langa exe: config.InstallDir() il vede si datele stau in <folder>\data (config.Default).
  La prima pornire config.MoveToInstall muta ~/.hobstack acolo (doar daca acolo nu e o baza; la eroare ramane pe
  ~/.hobstack), apoi RewriteDataRoot pe caile vechi.
- Add/Remove Programs (HKCU\...\Uninstall\HobStack); dupa un update din aplicatie DisplayVersion se actualizeaza
  (autostart.SetInstalledVersion). Uninstall: opreste HobStack, sterge cheia Run, scurtaturile si fisierele; datele
  doar daca raspunzi Da (implicit Nu).
- `hobstack quit [--force]`: POST /api/app/quit, asteapta pana se elibereaza portul (30 s); 0 / 2 / 1.

## macOS: HobStack.app si bara de meniu (cerut: "si pe mac ... install in aplicatii si ... in bara de sus langa ceas")

- cmd/hobstack/menubar_darwin.go (darwin && cgo): NSStatusItem cu imagine template (menubar.png, 36 px pentru 18 pt),
  meniul Open HobStack / Quit HobStack (NSAlert cu agenti care lucreaza), NSApplicationActivationPolicyAccessory (fara
  Dock). init() blocheaza goroutine-ul main pe firul principal; pentru `start`, main ruleaza start() intr-o goroutine si
  [NSApp run] pe firul principal pana la stopMenuBar (dupa care face restart-ul / iesirea). Codul Objective-C e in
  preambul; //export-urile in menubar_exports_darwin.go. Fara cgo (CGO_ENABLED=0) nu e bara (tray_other.go).
- scripts/build-mac.sh (doar pe macOS): tar.gz arm64 / amd64 (cgo, MACOSX_DEPLOYMENT_TARGET=11.0, semnate ad-hoc) si
  HobStack-<v>.dmg cu HobStack.app universal (lipo; installer/macos/Info.plist cu LSUIElement; assets/hobstack.icns),
  semnat ad-hoc (codesign --deep -s -), cu link catre Applications. Nesemnat de Apple: prima deschidere cere Open.
- Update-ul inlocuieste binarul din Contents/MacOS, apoi selfupdate.afterSwap pune versiunea in Info.plist si
  semneaza din nou bundle-ul ad-hoc. Datele raman in ~/.hobstack.
- Pornit din Finder sau la login PATH-ul e minim: loginShellPath ia PATH-ul din `$SHELL -ilc` (doar cand TERM lipseste).
- release.yml: job `mac` pe macos-latest (build-mac.sh, artefact), job `release` pe Linux (teste, Linux / Windows,
  installerul NSIS, artefactul mac, scripts/checksums.sh, gh release).

## Linux: tray si `hobstack install`

- cmd/hobstack/tray_linux.go: fyne.io/systray (StatusNotifierItem prin DBus, Go pur; singura dependenta noua, cu
  godbus). Fara DISPLAY / WAYLAND_DISPLAY nu porneste. Clic = Open; Quit cu agenti: al doilea clic in 10 s.
- `hobstack install [--dir] [-y]`: copiaza binarul (implicit ~/.local/share/hobstack), hobstack.portable (datele in
  <folder>/data), hobstack.png, ~/.local/share/applications/hobstack.desktop, iconita pe Desktop (gio trusted),
  ~/.local/bin/hobstack; opreste instanta care ruleaza si porneste cea instalata. `hobstack uninstall [--delete-data]`.

## Doar ultima versiune (cerut: "sa nu ramana si .prev de la versiunea din primul update")

- La 30 s dupa ce o versiune de release a pornit, selfupdate.CleanupPrevious sterge <exe>.prev (si un .new ramas):
  .prev exista doar cat porneste versiunea noua. Copiile bazei de date raman (ultimele 3).
