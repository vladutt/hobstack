import { api } from "./api.js";
import { el, toast } from "./ui.js";

// app-settings.js: Settings > App: pornirea la login si Quit. HobStack ruleaza fara terminal (pe Windows consola
// e ascunsa), deci de aici se opreste; pornit din nou, deschide pagina asta.
export function appSettings() {
  const box = el("section", { class: "section app-settings" });
  let st = null;
  let busy = false;

  async function load() {
    try { st = await api("/api/app"); } catch (err) { if (err.status !== 503) toast(err.message); st = null; }
    draw();
  }

  async function setAutostart(on) {
    busy = true; draw();
    try { st = await api("/api/app/autostart", { method: "PUT", body: { enabled: on } }); } catch (err) { toast(err.message); }
    busy = false; draw();
  }

  async function quit(force) {
    try {
      await api("/api/app/quit", { method: "POST", body: { force } });
    } catch (err) {
      if (err.status === 409 && err.data && err.data.running) {
        const n = err.data.running;
        if (confirm(`${n} agent${n === 1 ? " is" : "s are"} working right now. Quit anyway? Their tasks go back to Queued and continue the next time HobStack starts.`)) quit(true);
        return;
      }
      toast(err.message);
      return;
    }
    document.body.replaceChildren(el("main", { class: "app-stopped" },
      el("div", { class: "logo", "aria-hidden": "true" }, "H"),
      el("h1", {}, "HobStack stopped"),
      el("p", { class: "hint" }, "Start it again to continue; this page opens by itself.")));
  }

  function draw() {
    if (!st) { box.replaceChildren(); box.hidden = true; return; }
    box.hidden = false;
    const toggle = el("input", { type: "checkbox", checked: st.autostart, disabled: busy || !st.supported,
      onchange: (e) => setAutostart(e.target.checked) });
    const where = st.os === "windows" ? "Windows" : st.os === "darwin" ? "macOS" : "your desktop";
    box.replaceChildren(
      el("h2", {}, "App"),
      el("p", { class: "hint" }, `HobStack runs in the background at ${location.origin}; you can close the terminal and this tab. Starting it again opens this page.`),
      st.os === "windows" && el("p", { class: "hint" }, "It also sits in the notification area next to the clock: click the H to open it, right-click to quit."),
      el("label", { class: "check toggle-row" }, toggle,
        el("span", { class: "grow" }, el("strong", {}, "Start HobStack when I log in"),
          el("span", { class: "hint" }, `${where} starts it in the background, without opening the browser.`))),
      el("p", { class: "hint" }, "Log: ", el("code", {}, st.logFile)),
      el("div", { class: "upd-actions" },
        el("button", { type: "button", class: "btn small danger", onclick: () => quit(false) }, "Quit HobStack")));
  }

  draw();
  load();
  return box;
}
