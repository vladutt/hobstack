# Tab-uri pentru proiecte

## De ce
Utilizatorul lucreaza cu mai multe proiecte deodata si vrea sa sara intre ele dintr-un clic, cu numele vizibil si
starea fiecaruia, fara Cmd+K sau lista de proiecte. Proba (branch spike/project-tabs) a fost aprobata: "arata bine".

## Comportament
- Un rand de tab-uri sub bara de sus, pe toate paginile (si Settings / Library), doar cand exista macar un tab.
- Un proiect deschis (#/p/<id>...) intra singur la coada; max 8 (la al 9-lea pleaca cel mai vechi care nu e curent).
- Tab: initiala colorata (toneOf), numele, punct teal cand proiectul lucreaza (status Running), numar amber cu "need you"
  (needsCount). x (sau clic cu rotita) inchide; daca e tab-ul curent, treci pe vecin (sau pe Proiecte).
- Fiecare tab tine minte ultima adresa din proiect (Stats, Team...), ca un tab de browser; clic = acolo.
- Cmd/Ctrl+1..9 sare la tab-ul N (in browser scurtatura e a browserului; merge in fereastra proprie).
- Proiectele sterse dispar din tab-uri. Starea se reciteste la 15 s cat pagina e vizibila si exista tab-uri.
- Settings > App: "Project tabs" pornit / oprit (localStorage, ca bara din stanga). Bara din stanga ramane.

## Abordari
1. Rand separat in bara de sus (ales): loc pentru nume, ca la browser; --topbar-h creste cu randul.
2. Tab-urile in bara de sus, pe acelasi rand: bara e deja plina pe proiect (sectiuni + butoane).
3. Doar bara din stanga marita: nu arata numele fara hover.

## Lasat celui care construieste
Lista si adresele in localStorage ("hobstack-tabs", "hobstack-tab-routes"); fara server.
