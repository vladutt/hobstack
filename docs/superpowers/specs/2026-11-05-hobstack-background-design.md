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
