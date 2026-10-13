import { el } from "./ui.js";
import { highlightInto, languageFor } from "./highlight.js";

// Markdown minimal pentru textele scrise de agenti (rapoarte, mesaje).
// Stie: paragrafe, liste (- * 1.), titluri (#), blocuri ``` si linii de diff (+/-),
// iar in linie: `cod` si **bold**. Totul se construieste cu el(), deci textul
// ramane text: un agent nu poate injecta HTML.
//
// stats (optional): Map cale -> { added, removed, kind } din tab-ul Changes;
// un `cod` care numeste un fisier modificat primeste langa el +N −M.

// fileStat: cauta fisierul numit intr-un `cod` (ex. "lang/ro/catalog.php" sau
// "views/x.blade.php:19"); caile din Changes sunt relative la folderul agentului
// (sau absolute), deci acceptam si potrivire pe sufix.
function fileStat(code, stats) {
  if (!stats || !stats.size) return null;
  const name = code.trim().replace(/:\d+(-\d+)?$/, "").replace(/^\.\//, "");
  if (!name || /\s/.test(name)) return null;
  if (stats.has(name)) return stats.get(name);
  for (const [path, s] of stats) {
    if (path.endsWith("/" + name) || name.endsWith("/" + path)) return s;
  }
  return null;
}

// statBadge: "+3 −1" (si "new" pentru fisierele create)
export function statBadge(s) {
  return el("span", { class: "md-stat" },
    s.kind === "create" ? el("span", { class: "change-new" }, "new") : null,
    el("span", { class: s.added ? "change-add" : "change-add zero" }, `+${s.added}`),
    el("span", { class: s.removed ? "change-del" : "change-del zero" }, `−${s.removed}`));
}

// plain: text simplu; caile de fisiere modificate scrise fara `` primesc si ele +N −M
function plain(text, stats) {
  if (!stats || !stats.size) return [text];
  const out = [];
  const re = /[\w.\-/\\]+\.\w+(:\d+)?/g;
  let last = 0, m;
  while ((m = re.exec(text))) {
    const s = fileStat(m[0], stats);
    if (!s) continue;
    if (m.index > last) out.push(text.slice(last, m.index));
    out.push(el("span", { class: "md-file" }, el("span", { class: "md-path" }, m[0]), statBadge(s)));
    last = re.lastIndex;
  }
  if (last < text.length) out.push(text.slice(last));
  return out;
}

// inline: `cod` si **bold**; restul ramane text
function inline(text, stats) {
  const out = [];
  const re = /`([^`]+)`|\*\*([^*]+)\*\*/g;
  let last = 0, m;
  while ((m = re.exec(text))) {
    if (m.index > last) out.push(...plain(text.slice(last, m.index), stats));
    if (m[1] != null) {
      const s = fileStat(m[1], stats);
      out.push(s ? fileLabel(m[1], s) : el("code", {}, m[1]));
    } else {
      out.push(el("strong", {}, ...inline(m[2], stats)));
    }
    last = re.lastIndex;
  }
  if (last < text.length) out.push(...plain(text.slice(last), stats));
  return out;
}

// paragraf: liniile simple raman pe randuri separate (agentii scriu des asa)
function para(lines, stats) {
  const kids = [];
  lines.forEach((l, i) => {
    if (i) kids.push(el("br"));
    kids.push(...inline(l, stats));
  });
  return el("p", {}, kids);
}

function diffLine(l) {
  const cls = l[0] === "+" ? "diff-add" : l[0] === "-" ? "diff-del" : "diff-ctx";
  return el("div", { class: `diff-line ${cls}` }, l);
}

const LIST = /^\s*([-*]|\d+[.)])\s+(.*)$/;
const DIFF = /^[+-](\s|$)/;

// renderMarkdown intoarce un <div class="md"> cu textul formatat.
export function renderMarkdown(text, stats) {
  const root = el("div", { class: "md" });
  const lines = String(text || "").replace(/\r\n?/g, "\n").split("\n");
  let i = 0;
  while (i < lines.length) {
    const line = lines[i];
    if (!line.trim()) { i++; continue; }

    // bloc ``` ... ```
    if (/^\s*```/.test(line)) {
      const tag = (/^\s*```\s*([\w#+.-]*)/.exec(line)[1] || "").toLowerCase(); // ```php -> "php"
      const body = [];
      i++;
      while (i < lines.length && !/^\s*```/.test(lines[i])) body.push(lines[i++]);
      i++; // ``` de inchidere (sau sfarsitul textului)
      const diffy = body.some((l) => DIFF.test(l));
      if (!diffy) {
        // codul se coloreaza dupa eticheta (```php, ```ts); fara eticheta, ghicit
        const code = el("code", {});
        highlightInto(code, body.join("\n"), tag ? (languageFor("x." + tag) || tag) : null);
        root.append(el("pre", { class: "md-pre" }, code));
        continue;
      }
      root.append(el("pre", { class: "md-pre diff" }, body.map(diffLine)));
      continue;
    }

    // linii de diff lipite in text: un sir de linii "+ "/"- " in care apare macar un "+"
    // (altfel e o lista obisnuita cu "-")
    if (DIFF.test(line)) {
      let j = i;
      while (j < lines.length && DIFF.test(lines[j])) j++;
      const body = lines.slice(i, j);
      if (body.some((l) => l[0] === "+")) {
        root.append(el("div", { class: "md-pre diff" }, body.map(diffLine)));
        i = j;
        continue;
      }
    }

    const h = /^(#{1,6})\s+(.*)$/.exec(line);
    if (h) {
      root.append(el("p", { class: "md-h" }, inline(h[2], stats)));
      i++;
      continue;
    }

    if (LIST.test(line)) {
      const ordered = /^\s*\d/.test(line);
      const start = ordered ? parseInt(line, 10) : null; // "2." ramane 2, nu devine 1
      const items = [];
      while (i < lines.length && lines[i].trim()) {
        const m = LIST.exec(lines[i]);
        if (m && (/^\d/.test(m[1]) === ordered)) items.push([m[2]]);
        else if (m) break; // alt tip de lista: incepe un bloc nou
        else if (items.length) items[items.length - 1].push(lines[i].trim()); // continuarea elementului
        i++;
      }
      root.append(el(ordered ? "ol" : "ul", { start: start > 1 ? start : null }, items.map((it) => el("li", {}, para(it, stats)))));
      continue;
    }

    const body = [];
    while (i < lines.length && lines[i].trim() && !LIST.test(lines[i]) && !/^\s*```/.test(lines[i])
      && !/^#{1,6}\s/.test(lines[i]) && !DIFF.test(lines[i])) body.push(lines[i++]);
    if (!body.length) body.push(lines[i++]); // linie "+ x" fara pereche: ramane text
    root.append(para(body, stats));
  }
  return root;
}

// changeStats: Map cale -> { added, removed, kind, path, open? } din raspunsul /changes
// (acelasi fisier din mai multe run-uri se aduna). onOpen(path), daca e dat, face eticheta clicabila.
export function changeStats(data, onOpen) {
  const stats = new Map();
  for (const run of data?.runs || []) {
    for (const f of run.files || []) {
      const s = stats.get(f.path) || { added: 0, removed: 0, kind: f.kind, path: f.path, open: onOpen ? () => onOpen(f.path) : null };
      s.added += f.added;
      s.removed += f.removed;
      if (f.kind === "create") s.kind = "create";
      stats.set(f.path, s);
    }
  }
  return stats;
}

// fileLabel: `cale` +N −M; cu s.open, clicabila (deschide modala de fisier)
function fileLabel(code, s) {
  if (!s.open) return el("span", { class: "md-file" }, el("code", {}, code), statBadge(s));
  return el("span", { class: "md-file md-file-link", role: "button", tabindex: "0", title: "View file",
    onclick: () => s.open(), onkeydown: (e) => { if (e.key === "Enter") s.open(); } }, el("code", {}, code), statBadge(s));
}

// userText: textul unui mesaj al utilizatorului. Ramane text simplu (nu markdown), doar blocurile ```
// devin cod colorat, ca un fragment lipit sa se citeasca.
export function userText(text) {
  const parts = text.split(/^\s*```[^\n]*$/m);
  if (parts.length < 3) return document.createTextNode(text);
  const frag = document.createDocumentFragment();
  const tags = [...text.matchAll(/^\s*```([^\n]*)$/gm)].map((m) => m[1].trim());
  parts.forEach((part, i) => {
    const inCode = i % 2 === 1 && i < parts.length - 1; // un ``` neinchis ramane text
    if (!inCode) {
      const t = part.replace(/^\n|\n$/g, "");
      if (t) frag.append(t);
      return;
    }
    const tag = tags[i - 1] || "";
    const code = el("code", {});
    highlightInto(code, part.replace(/^\n|\n$/g, ""), tag ? (languageFor("x." + tag) || tag) : null);
    frag.append(el("pre", { class: "md-pre" }, code));
  });
  return frag;
}

// collapsible: un bloc lung (cod lipit, mesaj lung) se arata restrans la `lines` linii, cu "Show all"
// / "Show less" sub el. total = cate linii are de fapt (pentru eticheta).
export function collapsible(node, lines, total) {
  node.classList.add("fold");
  node.style.setProperty("--fold-lines", lines);
  const btn = el("button", { type: "button", class: "fold-toggle" });
  const paint = () => {
    const open = node.classList.contains("open");
    btn.textContent = open ? "Show less" : `Show all · ${total} lines`;
    btn.setAttribute("aria-expanded", String(open));
  };
  btn.addEventListener("click", () => { node.classList.toggle("open"); paint(); });
  paint();
  return el("div", { class: "fold-wrap" }, node, btn);
}
