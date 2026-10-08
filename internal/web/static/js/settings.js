import { el, topbar } from "./ui.js";
import { globalCommands } from "./commands.js";

// settingsView: setarile aplicatiei (deocamdata regulile pentru comenzi).
export function settingsView(root) {
  root.replaceChildren(
    topbar("settings"),
    el("main", { class: "settings-page" },
      el("h1", {}, "Settings"),
      globalCommands()));
}
