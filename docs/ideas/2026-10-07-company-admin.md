# Idee: admin self-hosted pentru companii (echipe de oameni)

Stare: idee de viitor, nimic implementat. Schimba directia "personal, NU SaaS" doar prin
adaugarea unui server optional; clientul local ramane ca acum.

## Ce vrea utilizatorul
- O companie instaleaza un admin pe serverul ei si il da angajatilor si echipelor.
- In admin se fac echipe de oameni si se trimit invitatii; fiecare om foloseste Foreman local,
  conectat cu un cont la adminul companiei.
- Motivul imediat: teste cu oameni care lucreaza in echipa pe proiecte asemanatoare.

## Reguli fixe
- Adminul NU ruleaza nimic pe masinile oamenilor: nu da comenzi, nu porneste task-uri sau
  run-uri. Seteaza doar limite (per echipa / per om) si setari pentru echipe sau proiecte.
- Configuratia coboara de la admin la client; clientul arata ce s-a schimbat, iar schimbarile
  de permisiuni le accepta utilizatorul.
- Conturile de model raman locale, la fiecare om: contul lui de Claude sau un model local
  (ex. Ollama). Adminul nu tine si nu distribuie credentiale.
- Ce trimite clientul inapoi e o setare a companiei, implicit "doar cifre" (run-uri, cost, stare).
  Codul, chat-urile si diff-urile nu pleaca decat daca firma alege explicit audit, iar clientul
  arata clar ce trimite.

## Forma tehnica propusa
- Acelasi binar Go, alt mod (ex. `foreman admin --listen ...`); SQLite sau Postgres.
- Protocol client <-> admin versionat de la prima zi (serverul si clientii se actualizeaza separat).
- Autentificare: la inceput invitatii pe email cu token; SSO (Google, Microsoft, Okta) mai tarziu.

## De verificat
- Ollama: Foreman depinde de Claude Code CLI (stream-json, MCP, permisiuni). De verificat daca
  Claude Code merge cu un endpoint local compatibil (ANTHROPIC_BASE_URL) sau daca trebuie un runner
  separat; si daca modelele locale fac fata orchestrarii prin tool-uri (lideri, task-uri, review).

## Ordinea
1. Sabloane de echipa si reguli ca fisiere (ex. `.foreman/team.json` + fisier de reguli), format
   stabil si versionat: exact ce va distribui adminul mai tarziu. (Roadmap punctul 2.)
2. Teste cu oameni, cu git ca mod de distributie a sabloanelor.
3. Modul admin (echipe, invitatii, limite, distributia sabloanelor, cifre inapoi) doar daca
   testele arata ca e nevoie de el.
