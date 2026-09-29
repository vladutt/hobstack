import { el } from "./ui.js";

// liveRenderer intoarce o functie care adauga in box evenimentele stream-json
// ale unui run: textul agentului si tool call-urile, restranse pe cate un rand.
export function liveRenderer(box) {
  const tools = new Map(); // tool_use_id -> <details>
  return function render(ev) {
    switch (ev.type) {
      case "app_start":
        box.append(el("details", {},
          el("summary", {}, "Mesajul primit de agent"),
          el("pre", {}, ev.prompt || "")));
        break;
      case "assistant":
        for (const block of ev.message?.content || []) {
          if (block.type === "text" && block.text.trim()) {
            box.append(el("div", { class: "live-text" }, block.text));
          } else if (block.type === "tool_use") {
            const d = el("details", {},
              el("summary", {}, `${toolName(block.name)} ${hint(block.input)}`),
              el("pre", {}, JSON.stringify(block.input, null, 2)));
            tools.set(block.id, d);
            box.append(d);
          }
        }
        break;
      case "user":
        for (const block of ev.message?.content || []) {
          if (block.type !== "tool_result") continue;
          const d = tools.get(block.tool_use_id);
          if (!d) continue;
          if (block.is_error) d.querySelector("summary").classList.add("err");
          d.append(el("pre", {}, truncate(resultText(block.content), 3000)));
        }
        break;
      case "result":
        if (ev.is_error) {
          box.append(el("div", { class: "err" }, "Eroare: " + ((ev.errors || []).join("; ") || ev.result || ev.subtype)));
        } else if (ev.total_cost_usd != null) {
          box.append(el("div", { class: "hint" }, `Gata, $${ev.total_cost_usd.toFixed(4)}`));
        }
        break;
      case "app_stderr":
        box.append(el("div", { class: "err" }, ev.text));
        break;
    }
  };
}

function toolName(name) {
  return name.replace(/^mcp__team__/, "echipa: ");
}

// un indiciu scurt langa numele tool-ului: fisierul, comanda, titlul task-ului
function hint(input = {}) {
  const h = input.file_path || input.command || input.pattern || input.title || input.task || input.url || "";
  return truncate(String(h).split("\n")[0], 80);
}

function resultText(content) {
  if (typeof content === "string") return content;
  if (Array.isArray(content)) return content.map((c) => c.text ?? JSON.stringify(c)).join("\n");
  return JSON.stringify(content, null, 2);
}

function truncate(s, n) {
  return s.length > n ? s.slice(0, n) + " [...]" : s;
}
