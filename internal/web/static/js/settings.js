import { el, topbar } from "./ui.js";
import { globalCommands } from "./commands.js";
import { accountsSettings } from "./accounts.js";

// settingsView: setarile aplicatiei: conturile Claude si regulile pentru comenzi.
export function settingsView(root) {
  root.replaceChildren(
    topbar("settings"),
    el("main", { class: "settings-page" },
      el("h1", {}, "Settings"),
      accountsSettings(),
      globalCommands()));
}
