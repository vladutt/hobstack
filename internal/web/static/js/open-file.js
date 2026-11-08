import { el, toast } from "./ui.js";
import { api } from "./api.js";

// Butoanele Open / Show in Finder pentru un fisier atasat (pe task sau in chat). HobStack ruleaza pe aceeasi
// masina cu browserul, deci serverul deschide fisierul acolo: originalul din proiect cat timp exista, altfel copia.

// Ca openableExt din internal/web/api_open_file.go: doar tipurile care nu pornesc nimic la un clic.
const OPENABLE = new Set(["png", "jpg", "jpeg", "gif", "webp", "avif", "bmp", "svg", "pdf", "txt", "md", "csv", "tsv",
  "json", "log", "html", "htm", "docx", "xlsx", "pptx", "odt", "ods", "mp4", "webm", "mov", "mp3", "wav"]);

export const canOpen = (name) => OPENABLE.has((name.split(".").pop() || "").toLowerCase());

// revealLabel: numele managerului de fisiere al sistemului.
export function revealLabel() {
  const ua = navigator.userAgent;
  if (/Mac/.test(ua)) return "Show in Finder";
  if (/Win/.test(ua)) return "Show in Explorer";
  return "Show in folder";
}

// openButtons(base, name): base = "/api/task-files/12" sau "/api/files/7". cls: clasa butoanelor.
export function openButtons(base, name, cls = "btn ghost small") {
  const call = async (what) => {
    try {
      const res = await api(`${base}/${what}`, { method: "POST", body: {} });
      if (!res.original) toast("The original file is gone; opened HobStack's copy.", "ok");
    } catch (err) { toast(err.message); }
  };
  return [
    canOpen(name) ? el("button", { type: "button", class: cls, title: "Open with the default app", onclick: (e) => { e.stopPropagation(); call("open"); } }, "Open") : null,
    el("button", { type: "button", class: cls, title: "Open its folder with the file selected", onclick: (e) => { e.stopPropagation(); call("reveal"); } }, revealLabel()),
  ].filter(Boolean);
}
