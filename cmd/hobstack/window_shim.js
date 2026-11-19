// Pus de fereastra Windows (WebView2) in fiecare pagina, inainte de scripturile ei. Pe macOS acelasi lucru il fac
// delegatii WKWebView. Paginile externe si fisierele aplicatiei deschise in "tab nou" merg in browserul de sistem
// (mesaj catre Go, verificat acolo din nou: parseBridge), restul paginilor aplicatiei raman in aceeasi fereastra (o
// singura fereastra), titlul paginii devine titlul ferestrei.
(() => {
  // doar pagina principala: un iframe (preview-ul HTML din explorer) nu vorbeste cu fereastra
  if (window.top !== window) return;
  window.hobstackApp = { platform: "windows" };
  const post = (msg) => {
    try { window.chrome.webview.postMessage(JSON.stringify(msg)); } catch (_) { /* fara punte: nimic */ }
  };

  // go: true = am tratat noi adresa. newWindow = cerut ca tab / fereastra noua.
  const go = (raw, newWindow) => {
    let u;
    try { u = new URL(raw, location.href); } catch (_) { return false; }
    if (u.origin !== location.origin) {
      if (!/^(https?|mailto):$/.test(u.protocol)) return false;
      post({ type: "open", url: u.href });
      return true;
    }
    if (!newWindow) return false;
    if (u.pathname.startsWith("/api/")) post({ type: "open", url: u.href }); // fisier: in browser, ca pe macOS
    else location.href = u.href; // alta pagina a aplicatiei: aici
    return true;
  };

  // la bubble pe window: dupa handler-ele paginii (un clic tratat de pagina are defaultPrevented)
  const onClick = (e) => {
    if (e.defaultPrevented || (e.type === "click" && e.button !== 0) || (e.type === "auxclick" && e.button !== 1)) return;
    const a = e.target instanceof Element ? e.target.closest("a[href]") : null;
    if (!a) return;
    // descarcarile din pagina le ia app.js (salvate in Downloads de server)
    if (a.hasAttribute("download") && new URL(a.href, location.href).origin === location.origin) return;
    const newWindow = a.target === "_blank" || e.ctrlKey || e.shiftKey || e.type === "auxclick";
    if (go(a.href, newWindow)) e.preventDefault();
  };
  window.addEventListener("click", onClick);
  window.addEventListener("auxclick", onClick);

  const open = window.open;
  window.open = function (raw, target, features) {
    if (raw && go(String(raw), true)) return null;
    return open.call(window, raw, target, features);
  };

  // un fisier / link tras peste un loc care nu-l primeste ar duce fereastra (fara bara de adrese) pe el
  window.addEventListener("dragover", (e) => { if (!e.defaultPrevented) e.preventDefault(); });
  window.addEventListener("drop", (e) => { if (!e.defaultPrevented) e.preventDefault(); });

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
