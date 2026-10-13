// Fisiere lipite cu Cmd+V (capturi de ecran, imagini copiate) si miniaturi pentru ele.
import { el, toast } from "./ui.js";
import { api } from "./api.js";

const MAX_SIZE = 50 << 20; // ca la upload

// uniqueName: imaginile lipite vin adesea cu numele generic "image.png"; primesc "screenshot-HHMMSSmmm.<ext>",
// ca doua lipiri la rand sa nu para acelasi fisier
function uniqueName(f) {
  const generic = f.type.startsWith("image/") && (!f.name || /^image\.\w+$/.test(f.name));
  if (!generic) return f;
  const ext = { "image/jpeg": "jpg", "image/gif": "gif", "image/webp": "webp" }[f.type] || "png";
  const d = new Date();
  const t = d.toTimeString().slice(0, 8).replaceAll(":", "") + String(d.getMilliseconds()).padStart(3, "0");
  return new File([f], `screenshot-${t}.${ext}`, { type: f.type });
}

// fromSystem: Firefox da paginii doar primul fisier copiat din Finder / Explorer. Serverul (local) citeste
// clipboard-ul sistemului; daca acolo sunt mai multe fisiere si printre ele e cel primit de la browser, se iau
// de acolo. null = raman fisierele browserului (captura de ecran, aceeasi lista, eroare).
async function fromSystem(browserFiles) {
  let list;
  try {
    list = (await api("/api/clipboard/files", { method: "POST" })).files || [];
  } catch { return null; }
  if (list.length <= browserFiles.length || !list.some((f) => f.name === browserFiles[0].name)) return null;
  const big = list.filter((f) => f.tooBig || f.size > MAX_SIZE);
  if (big.length) toast(`Skipped ${big.map((f) => f.name).join(", ")}: over 50 MB`);
  try {
    return await Promise.all(list.filter((f) => !big.includes(f)).map(async (f) => {
      const res = await fetch("/api/clipboard/file", {
        method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ path: f.path }),
      });
      if (!res.ok) throw new Error(res.statusText);
      const blob = await res.blob();
      return new File([blob], f.name, { type: blob.type === "application/octet-stream" ? "" : blob.type });
    }));
  } catch { return null; }
}

// onPasteFiles: la paste cu fisiere pe node cheama onFiles(lista). Daca e si text (ex. copiat din Word),
// textul se lipeste normal si fisierele se adauga oricum. Fara fisiere: paste-ul obisnuit, neatins.
export function onPasteFiles(node, onFiles) {
  node.addEventListener("paste", async (e) => {
    const files = Array.from(e.clipboardData?.files || []);
    if (!files.length) return;
    if (!e.clipboardData.getData("text/plain")) e.preventDefault();
    const all = await fromSystem(files);
    onFiles(all || files.map(uniqueName));
  });
}

// miniaturile: un URL blob per fisier, eliberat cu releaseThumb cand fisierul iese din lista
const thumbs = new WeakMap();

// fileThumb: <img> mic pentru o imagine, null pentru restul
export function fileThumb(f) {
  if (!f.type.startsWith("image/")) return null;
  let url = thumbs.get(f);
  if (!url) { url = URL.createObjectURL(f); thumbs.set(f, url); }
  return el("img", { class: "file-thumb", src: url, alt: "" });
}

export function releaseThumb(f) {
  const url = thumbs.get(f);
  if (url) { URL.revokeObjectURL(url); thumbs.delete(f); }
}
