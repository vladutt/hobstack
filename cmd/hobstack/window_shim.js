// Pus de fereastra Windows (WebView2) in fiecare pagina, inainte de scripturile ei. Pe macOS acelasi lucru il fac
// delegatii WKWebView. Paginile externe merg in browserul de sistem (mesaj catre Go, verificat acolo din nou),
// cele interne raman in aceeasi fereastra (o singura fereastra), titlul paginii devine titlul ferestrei.
(() => {
  window.hobstackApp = { platform: "windows" };
  const post = (msg) => {
    try { window.chrome.webview.postMessage(JSON.stringify(msg)); } catch (_) { /* fara punte: nimic */ }
  };
  const external = (u) => u.origin !== location.origin && /^(https?|mailto):$/.test(u.protocol);

  // go: true = am tratat noi adresa (externa in browser, interna in fereastra asta)
  const go = (raw, newWindow) => {
    let u;
    try { u = new URL(raw, location.href); } catch (_) { return false; }
    if (external(u)) {
      post({ type: "open", url: u.href });
      return true;
    }
    if (newWindow && u.origin === location.origin) {
      location.href = u.href;
      return true;
    }
    return false;
  };

  const onClick = (e) => {
    if (e.defaultPrevented || (e.type === "click" && e.button !== 0) || (e.type === "auxclick" && e.button !== 1)) return;
    const a = e.target instanceof Element ? e.target.closest("a[href]") : null;
    if (!a) return;
    // descarcarile din pagina le ia app.js (salvate in Downloads de server)
    if (a.hasAttribute("download") && new URL(a.href, location.href).origin === location.origin) return;
    const newWindow = a.target === "_blank" || e.ctrlKey || e.shiftKey || e.metaKey || e.type === "auxclick";
    if (go(a.href, newWindow)) e.preventDefault();
  };
  window.addEventListener("click", onClick, true);
  window.addEventListener("auxclick", onClick, true);

  const open = window.open;
  window.open = function (raw, target, features) {
    if (raw && go(String(raw), true)) return null;
    return open.call(window, raw, target, features);
  };

  let last = null;
  const sendTitle = () => {
    if (document.title === last) return;
    last = document.title;
    post({ type: "title", title: last });
  };
  document.addEventListener("DOMContentLoaded", () => {
    sendTitle();
    new MutationObserver(sendTitle).observe(document.head, { subtree: true, childList: true, characterData: true });
  });
})();
