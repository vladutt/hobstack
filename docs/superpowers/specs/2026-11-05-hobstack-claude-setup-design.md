# HobStack: Claude Code instalat din aplicatie

Data: 2026-11-05
Status: cerut de utilizator dupa prima pornire pe Windows ("putem sa facem si noi automat pasii?"): claude.exe era in
%USERPROFILE%\.local\bin, dar nu in PATH, iar HobStack iesea cu eroare.

## Decizia

- HobStack porneste si fara Claude Code. claudecli.Find cauta in PATH, ~/.local/bin, ~/.claude/local, npm global
  (%APPDATA%\npm\claude.cmd; /opt/homebrew/bin, /usr/local/bin, ~/.npm-global/bin). Fara el: team.Hold (contor,
  impartit cu update-ul) pana il gaseste; runner.SetClaude il pune dupa instalare; Start / Check / AuthStatus intorc
  runner.ErrNoClaude cat lipseste.
- Pagina de start (claude-setup.js, peste aplicatie) cand claude lipseste sau nu e logat, cu trei pasi:
  1. Install Claude Code: installerul oficial, doar la clic, cu comanda afisata inainte si output-ul live
     (Windows: `powershell -ExecutionPolicy Bypass -Command "irm https://claude.ai/install.ps1 | iex"`, in rest
     `curl -fsSL https://claude.ai/install.sh | bash`; 10 min, ultimele 300 de linii, fara culori ANSI);
     "I installed it, check again" cauta iar.
  2. Add to PATH (optional, doar pentru terminal; HobStack ruleaza claude prin calea absoluta): Windows = PATH-ul de
     utilizator (HKCU) prin [Environment]::SetEnvironmentVariable, la final, restul neatins (anunta Windows-ul);
     macOS / Linux = o linie `export PATH="$HOME/.local/bin:$PATH"` in ~/.zshrc (bash: ~/.bash_profile pe macOS,
     ~/.bashrc pe Linux). "In PATH" = PATH-ul procesului sau cel salvat (registry / fisierul shell-ului).
  3. Log in: deschide un terminal cu `claude auth login` (Windows Start-Process, macOS Terminal prin osascript,
     Linux primul emulator gasit); pagina verifica la 3 s `claude auth status`.
  "Continue without logging in" (sessionStorage) pentru cine foloseste doar conturi cu token pe proiect.
- API: GET /api/claude, POST /api/claude/install | check | path | login.

## In afara

Instalarea fara clic, actualizarea lui Claude Code (o face claude singur), Claude Desktop ca sursa (pachetul lui e intern).
