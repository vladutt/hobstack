import hljs from "../vendor/highlight/highlight.min.js";

// Peste aceste limite nu coloram: un diff sau un fisier imens nu trebuie sa blocheze pagina.
const MAX_LINES = 5000;
const MAX_BYTES = 500 * 1024;

// extensie -> limbaj highlight.js (doar ce exista in build-ul "common")
const EXT = {
  php: "php", js: "javascript", mjs: "javascript", cjs: "javascript", jsx: "javascript",
  ts: "typescript", tsx: "typescript", go: "go", py: "python", rb: "ruby", java: "java",
  kt: "kotlin", swift: "swift", cs: "csharp", c: "c", h: "c", cpp: "cpp", hpp: "cpp", rs: "rust",
  sql: "sql", json: "json", yml: "yaml", yaml: "yaml", toml: "ini", ini: "ini", env: "ini",
  sh: "bash", bash: "bash", zsh: "bash", css: "css", scss: "scss", less: "less",
  html: "xml", htm: "xml", xml: "xml", svg: "xml", vue: "xml", svelte: "xml", twig: "xml",
  md: "markdown", diff: "diff", patch: "diff", lua: "lua", pl: "perl", r: "r",
};

// languageFor: limbajul dupa numele fisierului ("x.blade.php" -> php); null = necunoscut
export function languageFor(path) {
  const name = String(path || "").split(/[\\/]/).pop().toLowerCase();
  if (name === "dockerfile") return hljs.getLanguage("dockerfile") ? "dockerfile" : null;
  if (name === "makefile") return hljs.getLanguage("makefile") ? "makefile" : null;
  const ext = name.includes(".") ? name.split(".").pop() : "";
  const lang = Object.hasOwn(EXT, ext) ? EXT[ext] : null; // nu "constructor", "__proto__" din Object.prototype
  return lang && hljs.getLanguage(lang) ? lang : null;
}

function tooBig(text) {
  if (text.length > MAX_BYTES) return true;
  let n = 0;
  for (let i = 0; i < text.length; i++) if (text.charCodeAt(i) === 10 && ++n > MAX_LINES) return true;
  return false;
}

// highlightInto: textul colorat in el (highlight.js scapa textul; nu intra HTML din continut).
// language null = ghicit dintre limbajele uzuale; text prea mare = text simplu.
export function highlightInto(el, text, language) {
  text = String(text ?? "");
  if (tooBig(text)) { el.textContent = text; return; }
  try {
    const res = language && hljs.getLanguage(language)
      ? hljs.highlight(text, { language, ignoreIllegals: true })
      : hljs.highlightAuto(text);
    el.innerHTML = res.value;
    el.classList.add("hljs");
  } catch {
    el.textContent = text;
  }
}

// highlightLine: o singura linie de cod (fara prefixul +/-), ca fragment; necunoscut = text simplu
export function highlightLine(text, language) {
  if (!language || !hljs.getLanguage(language) || text.length > 2000) return text;
  try {
    const t = document.createElement("template");
    t.innerHTML = hljs.highlight(text, { language, ignoreIllegals: true }).value;
    return t.content;
  } catch {
    return text;
  }
}
