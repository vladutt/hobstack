# Foreman: folder de lucru usor si fisiere atasate

Data: 2026-09-29
Status: design aprobat in chat. Utilizatorul a cerut trecerea direct la implementare.

## Scop

1. **Folderul de lucru** nu mai trebuie sa existe dinainte. Campul e optional:
   - daca il lasi gol, se creeaza automat un folder pentru proiect;
   - daca scrii o cale care nu exista, se creeaza;
   - butonul „Alege...” deschide dialogul nativ de ales folder.
2. **Fisiere atasate:**
   - la proiect, intr-o sectiune „Fisiere” vazuta de toata echipa;
   - la mesajele din chat (screenshot-uri, documente).
   Agentii le pot citi, inclusiv imagini si PDF-uri, cu tool-ul lor `Read`.

## Decizii

- **Folderul implicit** e `~/Foreman/<slug>`. Slug-ul se obtine din numele proiectului: litere mici, cifre si `-`, fara diacritice. Pe un nume gol sau fara caractere valide, slug-ul devine `proiect`. Daca folderul exista deja, se adauga `-2`, `-3` si asa mai departe.
- **O cale data explicit** (absoluta sau cu `~`) se creeaza cu `MkdirAll` daca nu exista. Daca exista, dar nu e folder, primesti eroare 400.
- **„Alege...”** apeleaza `POST /api/pick-folder`. Serverul deschide dialogul nativ:
  - macOS: `osascript` cu `choose folder`;
  - Windows: FolderBrowserDialog prin PowerShell;
  - Linux: `zenity --file-selection --directory`.
  Cancel intoarce 204. Un sistem fara dialog intoarce 501, iar `GET /api/info` raporteaza `canPickFolder`, ca UI-ul sa ascunda butonul.
- **Fisierele** stau in `~/.foreman/projects/<id>/files/`, deci niciodata in repo-ul proiectului. Numele pe disc e `<16 hex aleatori>-<nume curatat>`.
- **In SQLite**, tabel nou `attachments`:
  - `id`, `project_id`, `message_id` (NULL pentru fisierele de proiect);
  - `kind` (`project` sau `message`);
  - `name` (numele original), `stored`, `size`, `mime`, `created_at`.
  Se sterge in cascada cu proiectul sau cu mesajul.
- **Limita:** 50 MB per fisier si 200 MB per request.
- **Agentii primesc acces la fisiere:**
  - fiecare run primeste `--add-dir <folderul de fisiere al proiectului>`. `Read` e deja permis; proba din 2026-09-29 a confirmat citirea unui text si a unui PNG din folderul adaugat;
  - mesajul fiecarei ture contine blocul `Fisiere proiect (le poti citi cu Read):`, cu maxim 50 de fisiere de proiect, fiecare cu calea completa si numele original. System prompt-ul ramane neschimbat, ca sesiunile sa nu se reseteze la fiecare fisier nou;
  - un mesaj cu atasamente ajunge la agent ca `<text>\nAtasat: <cale> (<nume>)`, cate o linie per fisier.
- **Securitate:**
  - upload-ul e `multipart/form-data`, pe care o pagina straina il poate trimite fara preflight. De aceea cererile multipart cer si header-ul `X-Foreman: 1`, pe care browserul nu-l trimite cross-site fara preflight;
  - fisierele se servesc cu `X-Content-Type-Options: nosniff` si `Content-Security-Policy: sandbox`;
  - `inline` se foloseste doar pentru imagini (png, jpeg, gif, webp), PDF si text simplu. Restul se servesc ca `attachment`, ca un HTML incarcat sa nu ruleze pe originea aplicatiei.
- **Stergere:**
  - stergerea unui fisier il sterge si de pe disc;
  - stergerea proiectului sterge si `~/.foreman/projects/<id>/`, dar niciodata folderul de lucru.

## API

| Ruta | Ce face |
|---|---|
| `GET /api/info` | `{name, canPickFolder}` |
| `POST /api/pick-folder` | `{path}`; 204 la cancel; 501 daca dialogul nu e suportat |
| `POST /api/projects` si `PUT` | `workdir` optional: gol inseamna implicitul; o cale inexistenta se creeaza |
| `GET /api/projects/{id}/files` | lista fisierelor de proiect |
| `POST /api/projects/{id}/files` | multipart cu campul `files` (unul sau mai multe) si `X-Foreman: 1`; 201 cu lista creata |
| `GET /api/files/{id}` | continutul fisierului |
| `DELETE /api/files/{id}` | 204 |
| `POST /api/agents/{id}/messages` | JSON `{text}` ca acum, sau multipart cu `text`, `files` si `X-Foreman: 1` |

- Mesajele (`GET` pe thread, raspunsul la `POST` si evenimentele SSE `message`) contin `attachments: [...]`.
- Un fisier incarcat in proiect declanseaza evenimentul SSE `files`.

## UI

- **Formularul de proiect:** campul de folder e optional, cu placeholder `gol = ~/Foreman/<nume>`. Butonul „Alege...” apare doar daca `canPickFolder` e true.
- **Workspace, sectiunea „Fisiere”:**
  - lista cu nume, marime, deschide si sterge;
  - buton „Incarca”;
  - drag & drop peste sectiune.
- **Chat:**
  - buton cu agrafa (input file multiplu), paste de imagini (Cmd+V) si drag & drop peste consola;
  - fisierele alese apar ca etichete cu x inainte de „Trimite”;
  - in mesaje, imaginile apar ca miniaturi (link spre original), iar restul ca link-uri.

## In afara acestei felii

- Agentii nu adauga fisiere in „Fisiere”.
- Fisierele nu se versioneaza.
- Nu exista preview pentru alte tipuri decat imagini.
