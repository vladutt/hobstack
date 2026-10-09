import { el } from "./ui.js";

// Cuvantul de dinaintea cursorului, daca e o mentiune inceputa: "@" la inceput sau dupa spatiu,
// apoi orice fara spatiu. Intoarce { start, query } sau null.
export function mentionAt(text, caret) {
  const before = text.slice(0, caret);
  const m = /(^|\s)@([^\s@]*)$/.exec(before);
  if (!m) return null;
  return { start: caret - m[2].length - 1, query: m[2] };
}

// attachMentions: @ in textarea deschide o lista cu fisierele proiectului.
// - input: textarea-ul; anchor: elementul deasupra caruia sta lista (pozitionat relativ)
// - search(query) -> Promise<[{ path, name, folder }]>
// Sageti / Enter / Tab aleg, Esc inchide; alegerea scrie "@cale " in locul mentiunii.
// Intoarce { close() }.
export function attachMentions(input, anchor, search) {
  const list = el("ul", { class: "mention-list", role: "listbox", hidden: true, "aria-label": "Project files" });
  anchor.append(list);
  let items = [];   // rezultatele aratate
  let active = 0;   // randul selectat
  let token = null; // mentiunea curenta { start, query }
  let seq = 0;      // ultima cerere: un raspuns vechi nu suprascrie unul nou
  let timer = null;

  function close() {
    list.hidden = true;
    list.replaceChildren();
    items = [];
    token = null;
    clearTimeout(timer);
    input.removeAttribute("aria-activedescendant");
  }

  function update() {
    const t = mentionAt(input.value, input.selectionStart ?? input.value.length);
    if (!t) { close(); return; }
    token = t;
    clearTimeout(timer);
    timer = setTimeout(async () => {
      const my = ++seq;
      let res = [];
      try { res = await search(t.query); } catch { res = []; }
      if (my !== seq || !token) return;
      items = res;
      active = 0;
      render();
    }, 120);
  }

  function render() {
    if (!items.length) {
      list.replaceChildren(el("li", { class: "mention-empty" }, token.query ? "No matching file" : "Type to search files"));
      list.hidden = false;
      return;
    }
    list.replaceChildren(...items.map((f, i) => {
      // folderul fara "/" final: cu direction rtl (taiere la stanga) slash-ul ar sari in fata
      const dir = f.path.slice(0, Math.max(0, f.path.length - f.name.length)).replace(/[\\/]+$/, "");
      return el("li", {
        id: `mention-${i}`, role: "option", class: "mention-item" + (i === active ? " active" : ""),
        "aria-selected": String(i === active),
        // mousedown, nu click: textarea-ul nu pierde focusul
        onmousedown: (e) => { e.preventDefault(); pick(i); },
      }, el("span", { class: "mention-name" }, f.name), el("span", { class: "mention-dir" }, dir || f.folder));
    }));
    list.hidden = false;
    input.setAttribute("aria-activedescendant", `mention-${active}`);
    list.children[active]?.scrollIntoView({ block: "nearest" });
  }

  function pick(i) {
    const f = items[i];
    if (!f || !token) return;
    const caret = input.selectionStart ?? input.value.length;
    const insert = "@" + f.path + " ";
    input.value = input.value.slice(0, token.start) + insert + input.value.slice(caret);
    const pos = token.start + insert.length;
    input.setSelectionRange(pos, pos);
    close();
    input.dispatchEvent(new Event("input", { bubbles: true })); // inaltimea textarea-ului, ciorna
    input.focus();
  }

  // capture: ruleaza inaintea Enter-ului care trimite mesajul
  input.addEventListener("keydown", (e) => {
    if (list.hidden || !items.length) {
      if (e.key === "Escape" && !list.hidden) { e.preventDefault(); close(); }
      return;
    }
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      active = (active + (e.key === "ArrowDown" ? 1 : items.length - 1)) % items.length;
      render();
    } else if (e.key === "Enter" || e.key === "Tab") {
      e.preventDefault();
      e.stopImmediatePropagation();
      pick(active);
    } else if (e.key === "Escape") {
      e.preventDefault();
      close();
    }
  }, true);
  input.addEventListener("input", update);
  input.addEventListener("click", update);
  input.addEventListener("keyup", (e) => { if (e.key === "ArrowLeft" || e.key === "ArrowRight") update(); });
  input.addEventListener("blur", () => setTimeout(close, 150));
  return { close };
}
