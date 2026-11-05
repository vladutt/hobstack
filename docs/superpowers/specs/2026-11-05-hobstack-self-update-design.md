# HobStack: versiuni, release-uri pe GitHub si update din aplicatie

Data: 2026-11-05
Status: cerut de utilizator: "update-ul ar trebui sa fie facut in functie de tag-ul de versiune. incepem de la 1.0.0,
iar cand apare o versiune noua, cand intri pe hobstack sa-ti apara o modala cu versiunea", plus build-ul pe GitHub ca
sa poata fi descarcat usor. Inlocuieste prima varianta (git pull + go build din folderul sursa).

## Versiuni

- Taguri semver `vX.Y.Z`, prima `v1.0.0`. `config.Version` se pune la build cu
  `-ldflags "-X hobstack/internal/config.Version=1.0.0"`; un `go build` simplu e "dev".
- `scripts/build.sh` (local): versiunea din `git describe --tags --always --dirty` (de ex. 1.0.0-3-g657c1cf).
- `hobstack version` scrie versiunea (o foloseste si update-ul ca proba pe binarul descarcat).
- Comparatia (selfupdate.Newer): X.Y.Z numeric; un release cu sufix (rc) nu se propune; la acelasi numar,
  release-ul e mai nou doar fata de o pre-versiune, nu fata de un build de dupa tag (sufixul git describe).

## Release pe GitHub

- `.github/workflows/release.yml` la push-ul unui tag `v*`: vet, teste, `scripts/build-release.sh`, `gh release create`.
- Arhive: `hobstack_<ver>_<os>_<arch>.tar.gz` (darwin arm64/amd64, linux amd64/arm64) si `.zip` pe windows/amd64,
  cu binarul, README si LICENSE in radacina; `checksums.txt` (sha256). CGO_ENABLED=0, -trimpath, -s -w.
- Notele: mesajul tag-ului adnotat; altfel commit-urile de la tag-ul precedent; la primul "First release of HobStack."

## Update din aplicatie (internal/selfupdate)

- Verificare: la 5 s dupa pornire si din ora in ora, `GET api.github.com/repos/vladutt/hobstack/releases/latest`
  (config.UpdateRepo; HOBSTACK_UPDATE_URL pentru teste). 404 = niciun release.
- Se poate instala daca release-ul are arhiva platformei si checksums.txt, iar folderul binarului se poate scrie.
- Pasi: checksums.txt, arhiva (max 300 MB) verificata cu sha256, binarul scos din arhiva in `<exe>.new`, proba
  `<exe>.new version` (20 s) trebuie sa raspunda exact versiunea, apoi `<exe>` -> `<exe>.prev` si `.new` -> `<exe>`.
  Orice eroare inainte de inlocuire lasa totul neschimbat.
- Apoi ca inainte: team.Hold (nu pornesc run-uri noi) pana nu mai ruleaza niciun run, copie VACUUM INTO in
  ~/.hobstack/backups (ultimele 3; daca nu merge nu reporneste), restart prin selfupdate.Reexec cu --no-open.
  "Cancel restart" lasa versiunea instalata pentru urmatoarea pornire ("Restart now" nu o mai descarca).
- Build "dev": nu propune singur, dar Settings > Version are "Install vX.Y.Z".

## UI (update.js)

- Modala la intrare (o data pe pagina) cand exista o versiune noua: "HobStack vX.Y.Z is available", versiunea curenta,
  data, notele (Markdown), link la release; Update and restart / Later (pana la urmatoarea sesiune, sessionStorage) /
  Skip this version (localStorage). In timpul update-ului arata pasul si agentii asteptati.
- Insigna din bara de sus: "vX.Y.Z available", pasul, "Waiting for N agents", "Update failed", "Restart to update".
- Settings > Version: versiunea, ultimul release, Check now, Release notes.
- Dupa restart pagina se reincarca singura si arata "HobStack updated to vX.Y.Z".

## In afara

Semnarea binarelor (macOS notarization), rollback din UI la `.prev`, canale beta.
