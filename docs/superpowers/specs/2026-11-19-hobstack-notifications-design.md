# Notificari de sistem

## De ce
Utilizatorul lasa echipele sa lucreze si face altceva. Vrea sa afle, fara sa se uite la board:
1. cand s-a terminat treaba pe un proiect care a avut de lucru;
2. cand apare ceva in "Needs you" (o intrebare, o cerere de acces, un task blocat...).

## Scopuri / non-scopuri
- Scop: o notificare de sistem (macOS, Windows, Linux) pentru cele doua momente, cu clic spre proiect.
- Non-scop: notificare pentru fiecare task terminat; sunete alese; notificari pe telefon.
- Anti-scop: spam. Daca vine cate o notificare la fiecare pauza intre doi agenti, sau trei notificari pentru acelasi
  moment, utilizatorul o opreste si functia e moarta.

## Abordari
1. **Serverul decide si trimite (ales).** In Go, un observator pe proiecte urmareste trecerea "lucreaza -> oprit" si
   aparitia elementelor noi in Needs you; trimite notificarea nativa a sistemului. Merge cu fereastra ascunsa, fara
   niciun tab deschis.
2. Pagina trimite (Notification API din browser). Respins: fereastra e de obicei ascunsa, tab-ul poate lipsi, iar
   WKWebView nu are Notification API.
3. Doar o insigna / numar pe iconita din Dock / tray. Respins: nu atrage atentia cand utilizatorul e in alta aplicatie.

## Comportament
**Treaba terminata.** Un proiect e "oprit" cand nu are niciun run activ si niciun task pe care un agent il poate lua
(Queued nestopat, In progress fara run care nu asteapta raspuns / aprobare / sub-task-uri). Starea trebuie sa tina
**60 s** (o pauza intre doi agenti nu conteaza); orice run nou in fereastra anuleaza. Se notifica doar daca de la
ultima notificare de acest fel a rulat macar un run pe task (un mesaj de chat singur nu face notificare).
- Titlu: "<Proiect> finished"; text: "N tasks done" (terminate de la ultima notificare) si, daca e cazul,
  "· M need you".
- Daca proiectul s-a oprit si are elemente in Needs you, titlul e "<Proiect> is waiting for you".

**Needs you.** Un element nou in Needs you (aceleasi categorii ca numarul "need you" din UI: intrebari, task-uri
blocate, cereri de acces, de agenti, de lucru intre proiecte, de servere MCP, propuneri Library, "To fill in", contul
Claude picat) da o notificare. Elementele noi aparute in **10 s** pe acelasi proiect se aduna intr-una singura.
- Un element: titlu "<Proiect> needs you", textul = randul scurt al cardului (ex. "Atlas asks: Which payment
  provider?", "Atlas wanted the skill "claude-api"").
- Mai multe: "<Proiect> needs you", "3 new items".
- Un element nu da notificare de doua ori (dupa id; numarul care creste la acelasi card nu conteaza).
- Cand o notificare "is waiting for you" ar veni in 10 s dupa una "needs you" pentru aceleasi elemente, ramane doar
  cea "needs you" (nu doua pentru acelasi moment).

**Fara notificare cand te uiti deja.** Daca fereastra proprie HobStack e in fata (focus) pe acel proiect, nu se trimite.
In browser nu stim sigur; se trimite.

**Clic** pe notificare: deschide fereastra HobStack (sau browserul, dupa preferinta "Open HobStack in") pe proiect.

**Setari.**
- Global, in Settings (sectiunea App, pornite implicit, in <date>/app.json ca preferintele ferestrei):
  "Notify me when a project finishes" si "Notify me when something needs me". Oprite = nicio notificare de acel fel.
- Per proiect, in formularul proiectului (Edit): "Notifications" pornit / oprit (coloana noua pe projects, implicit
  pornit, migrare). Oprit = proiectul nu da niciun fel de notificare; setarile globale raman cum sunt.

## Pe sisteme
- macOS: UNUserNotificationCenter cand ruleaza din HobStack.app (are app.hobstack; prima data macOS cere
  permisiunea). Binarul pornit direct (build local, tar.gz): `osascript display notification` ca rezerva, fara clic.
- Windows: balon / toast din iconita de langa ceas (Shell_NotifyIconW, NIF_INFO); clic pe balon = deschide.
- Linux: `notify-send` daca exista (clic: `--action` unde e suportat, altfel fara); lipsa = nimic, fara eroare.

## Lasat celui care construieste
Unde exact sta observatorul (probabil langa team / evenimentele din hub), cum se numara "N tasks done", textul exact
al randului scurt per tip de card, testele (logica de stare si de adunare testata fara sistem, cu un trimitator fals).
