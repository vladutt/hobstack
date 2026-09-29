// api face un request JSON catre server si arunca o eroare cu mesajul
// serverului (in romana) daca raspunsul nu e ok.
// Serverul cere Content-Type: application/json pe orice request care nu e GET
// (chiar si fara body), ca protectie impotriva request-urilor cross-site.
export async function api(path, { method = "GET", body } = {}) {
  const res = await fetch(path, {
    method,
    headers: method === "GET" ? {} : { "Content-Type": "application/json" },
    body: body ? JSON.stringify(body) : undefined,
  });
  if (res.status === 204 || res.status === 202) return null;
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error || res.statusText);
  return data;
}
