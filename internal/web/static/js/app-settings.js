import { api } from "./api.js";
import { el, toast } from "./ui.js";
import { railEnabled, setRailEnabled } from "./recent-rail.js";

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

  async function setOpenIn(openIn) {
    busy = true; draw();
    try { st = await api("/api/app/open-in", { method: "PUT", body: { openIn } }); } catch (err) { toast(err.message); }
    busy = false; draw();
  }

  // openInGroup: "Open HobStack in" App window / Browser (doar pe macOS / Windows, unde exista fereastra)
  function openInGroup() {
    if (!st.windowSupported && !st.windowReason) return null;
    const off = busy || !st.windowSupported;
    const option = (value, label, hint) => el("label", { class: "check toggle-row" },
      el("input", { type: "radio", name: "open-in", value, checked: st.openIn === value, disabled: off,
        onchange: () => setOpenIn(value) }),
      el("span", { class: "grow" }, el("strong", {}, label), el("span", { class: "hint" }, hint)));
    return el("div", { class: "open-in" },
      el("h3", {}, "Open HobStack in"),
      option("window", "App window", "Its own window, like any other app."),
      option("browser", "Browser", "A tab in your default browser."),
      !st.windowSupported && el("p", { class: "hint" }, `App window is not available: ${st.windowReason}`),
      el("p", { class: "hint" }, "Takes effect the next time you open HobStack."));
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
    // replaceChildren nativ scrie false / null ca text ("false" sub titlu pe macOS): le scoatem
    box.replaceChildren(...[
      el("h2", {}, "App"),
      el("p", { class: "hint" }, `HobStack runs in the background at ${location.origin}; you can close the terminal and this ${window.hobstackApp ? "window" : "tab"}. Starting it again opens this page.`),
      st.os === "windows" && el("p", { class: "hint" }, "It also sits in the notification area next to the clock: click the H to open it, right-click to quit."),
      el("label", { class: "check toggle-row" }, toggle,
        el("span", { class: "grow" }, el("strong", {}, "Start HobStack when I log in"),
          el("span", { class: "hint" }, `${where} starts it in the background, without opening the browser.`))),
      openInGroup(),
      window.hobstackApp && el("label", { class: "check toggle-row" },
        el("input", { type: "checkbox", checked: railEnabled(), onchange: (e) => setRailEnabled(e.target.checked) }),
        el("span", { class: "grow" }, el("strong", {}, "Recent projects on the side"),
          el("span", { class: "hint" }, "Up to 5 recently opened projects at the left edge of this window; click one to open it."))),
      el("p", { class: "hint" }, "Log: ", el("code", {}, st.logFile)),
      el("div", { class: "upd-actions" },
        el("button", { type: "button", class: "btn small danger", onclick: () => quit(false) }, "Quit HobStack"))].filter(Boolean));
  }

  draw();
  load();
  return box;
}
