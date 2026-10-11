// Fisiere lipite cu Cmd+V (capturi de ecran, imagini copiate) si miniaturi pentru ele.
import { el } from "./ui.js";

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

// onPasteFiles: la paste cu fisiere pe node cheama onFiles(lista). Daca e si text (ex. copiat din Word),
// textul se lipeste normal si fisierele se adauga oricum. Fara fisiere: paste-ul obisnuit, neatins.
export function onPasteFiles(node, onFiles) {
  node.addEventListener("paste", (e) => {
    const files = Array.from(e.clipboardData?.files || []);
    if (!files.length) return;
    if (!e.clipboardData.getData("text/plain")) e.preventDefault();
    onFiles(files.map(uniqueName));
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
