// api face un request JSON catre server si arunca o eroare cu mesajul
// serverului daca raspunsul nu e ok.
// Serverul cere Content-Type: application/json pe orice request care nu e GET
// (chiar si fara body), ca protectie impotriva request-urilor cross-site.
export async function api(path, { method = "GET", body } = {}) {
  const res = await fetch(path, {
    method,
    headers: method === "GET" ? {} : { "Content-Type": "application/json" },
    body: body ? JSON.stringify(body) : undefined,
  });
  if (res.status === 204) return null;
  // 202 poate veni cu corp (ex. sesiunea la /chat) sau fara (ex. /stop): fara corp -> null
  const data = await res.json().catch(() => (res.status === 202 ? null : {}));
  if (!res.ok) throw new Error(data.error || res.statusText);
  return data;
}

// upload trimite un FormData (fisiere). Nu setam Content-Type: browserul pune
// singur boundary-ul. Header-ul X-HobStack e cerut de server pe multipart,
// ca o pagina straina sa nu poata trimite upload-uri fara preflight.
export async function upload(path, formData) {
  const res = await fetch(path, {
    method: "POST",
    headers: { "X-HobStack": "1" },
    body: formData,
  });
  if (res.status === 204 || res.status === 202) return null;
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error || res.statusText);
  return data;
}
