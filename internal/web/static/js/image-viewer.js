import { el } from "./ui.js";
import { openButtons } from "./open-file.js";

// Poza atasata (chat) se deschide intr-o modala peste pagina, nu in alt tab. Cu mai multe poze in lista,
// sagetile stanga / dreapta trec intre ele; Esc sau clic pe fundal inchide.

let current = null; // { overlay, onKey }

// openImage(list, index): list = [{ src, name, base }]; base = "/api/files/7" pentru Open / Show in Finder.
export function openImage(list, index = 0) {
  closeImage();
  let i = index;
  const img = el("img", { class: "iv-img", alt: "" });
  const name = el("span", { class: "iv-name" });
  const count = el("span", { class: "iv-count" });
  const actions = el("span", { class: "iv-actions" });
  const prev = el("button", { type: "button", class: "iv-nav iv-prev", "aria-label": "Previous image", onclick: (e) => { e.stopPropagation(); go(-1); } }, "‹");
  const next = el("button", { type: "button", class: "iv-nav iv-next", "aria-label": "Next image", onclick: (e) => { e.stopPropagation(); go(1); } }, "›");
  const close = el("button", { type: "button", class: "iv-close", "aria-label": "Close", onclick: closeImage }, "×");

  function show() {
    const it = list[i];
    img.src = it.src;
    img.alt = it.name;
    name.textContent = it.name;
    count.textContent = list.length > 1 ? `${i + 1} / ${list.length}` : "";
    actions.replaceChildren(...openButtons(it.base, it.name),
      el("a", { class: "btn ghost small", href: `${it.src}?download=1`, download: it.name }, "Download"));
    prev.hidden = next.hidden = list.length < 2;
  }
  function go(d) {
    i = (i + d + list.length) % list.length;
    show();
  }
  const overlay = el("div", { class: "iv-overlay", role: "dialog", "aria-modal": "true", "aria-label": "Image",
    onclick: (e) => { if (e.target === overlay || e.target.classList.contains("iv-stage")) closeImage(); } },
  el("div", { class: "iv-bar" }, name, count, el("span", { class: "grow" }), actions, close),
  el("div", { class: "iv-stage" }, prev, img, next));
  // in faza de captura: Esc inchide doar poza, nu si sertarul sau modala de dedesubt
  const onKey = (e) => {
    if (e.key === "Escape") closeImage();
    else if (e.key === "ArrowLeft" && list.length > 1) go(-1);
    else if (e.key === "ArrowRight" && list.length > 1) go(1);
    else return;
    e.preventDefault();
    e.stopPropagation();
  };
  document.addEventListener("keydown", onKey, true);
  document.body.append(overlay);
  current = { overlay, onKey };
  show();
  close.focus();
}

export function closeImage() {
  if (!current) return;
  document.removeEventListener("keydown", current.onKey, true);
  current.overlay.remove();
  current = null;
}
