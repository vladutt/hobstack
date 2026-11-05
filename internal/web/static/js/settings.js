import { el, topbar } from "./ui.js";
import { globalCommands } from "./commands.js";
import { accountsSettings } from "./accounts.js";
import { versionSettings } from "./update.js";
import { appSettings } from "./app-settings.js";

// settingsView: setarile aplicatiei: conturile Claude, regulile pentru comenzi si versiunea (update).
export function settingsView(root) {
  root.replaceChildren(
    topbar("settings"),
    el("main", { class: "settings-page" },
      el("h1", {}, "Settings"),
      accountsSettings(),
      globalCommands(),
      appSettings(),
      versionSettings()));
}
