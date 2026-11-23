package main

import (
	"encoding/json"
	"math"
	"net/url"
	"strings"

	"hobstack/internal/appprefs"
)

// Logica ferestrei proprii care nu tine de platforma (testata si pe macOS): marimea la deschidere, zoom-ul
// citit din app.json si mesajele trimise de pagina (Windows: window_shim.js).

// winRect: un dreptunghi in pixelii ecranului (Windows: originea sus-stanga).
type winRect struct{ X, Y, W, H int }

const (
	defaultWinW = 1400 // marimea la prima deschidere (inainte de scalarea DPI)
	defaultWinH = 900
	minWinW     = 400 // sub atat marimea salvata nu se mai foloseste
	minWinH     = 300
)

// windowRect: marimea salvata daca e destul de mare si se vede pe un monitor (onScreen), altfel 1400x900
// scalat cu DPI-ul, limitat la zona de lucru si centrat in ea.
func windowRect(saved *appprefs.Frame, work winRect, scale float64, onScreen func(winRect) bool) winRect {
	if saved != nil && saved.W >= minWinW && saved.H >= minWinH {
		r := winRect{int(saved.X), int(saved.Y), int(saved.W), int(saved.H)}
		if onScreen(r) {
			return r
		}
	}
	if scale <= 0 || math.IsNaN(scale) {
		scale = 1
	}
	w := min(int(math.Round(defaultWinW*scale)), work.W)
	h := min(int(math.Round(defaultWinH*scale)), work.H)
	return winRect{work.X + (work.W-w)/2, work.Y + (work.H-h)/2, w, h}
}

// scaleFromDPI: scala ferestrei pentru un DPI de monitor (96 = 100%, 120 = 125%, 144 = 150%, 192 = 200%). Un DPI
// lipsa (0, de ex. GetDpiForWindow nu exista) inseamna 1. Aceeasi scala ajunge la WebView2 (RasterizationScale), deci
// marimea in pixeli fizici = marimea CSS x scala, ca in Edge la aceeasi scala Windows.
func scaleFromDPI(dpi uint32) float64 {
	if dpi == 0 {
		return 1
	}
	return float64(dpi) / 96
}

// clampZoom: zoom-ul din app.json (scris de mana sau de o versiune veche) intre 0.5 si 3; lipsa = 1.
func clampZoom(z float64) float64 {
	if z <= 0 || math.IsNaN(z) {
		return 1
	}
	return min(max(z, 0.5), 3)
}

// bridgeMsg: ce trimite pagina prin chrome.webview.postMessage (window_shim.js).
type bridgeMsg struct {
	Type  string `json:"type"`
	URL   string `json:"url"`
	Title string `json:"title"`
}

const maxTitleRunes = 200

// parseBridge decide ce face fereastra cu un mesaj de la pagina: "open" = adresa in browserul de sistem, "title" =
// titlul ferestrei, "" = nimic. In browser merg doar http/https catre alta origine, fisierele aplicatiei (/api/...,
// deschise cu target=_blank, ca pe macOS) si mailto. Pagina e a noastra, dar un link din markdown poate fi orice:
// nu deschidem file:, javascript: sau alte scheme. Adresa ajunge in linia de comanda a lui rundll32: o dam re-escapata.
func parseBridge(raw, origin string) (action, arg string) {
	var m bridgeMsg
	if json.Unmarshal([]byte(raw), &m) != nil {
		return "", ""
	}
	switch m.Type {
	case "open":
		u, err := url.Parse(m.URL)
		if err != nil {
			return "", ""
		}
		switch strings.ToLower(u.Scheme) {
		case "mailto":
			if strings.ContainsFunc(m.URL, func(r rune) bool { return r <= ' ' || r == '"' || r == 0x7f }) {
				return "", ""
			}
			return "open", m.URL
		case "http", "https":
			if u.Host == "" {
				return "", ""
			}
			if strings.EqualFold(u.Scheme+"://"+u.Host, origin) && !strings.HasPrefix(u.Path, "/api/") {
				return "", "" // o pagina a aplicatiei ramane in fereastra
			}
			return "open", u.String()
		}
	case "title":
		t := []rune(strings.TrimSpace(m.Title))
		if len(t) > maxTitleRunes {
			t = t[:maxTitleRunes]
		}
		return "title", string(t)
	}
	return "", ""
}

// windowHash: fragmentul adresei ("#/p/3") daca are doar caractere sigure de pus intr-un script
// (location.hash in fereastra deja deschisa); altfel "".
func windowHash(rawURL string) string {
	i := strings.IndexByte(rawURL, '#')
	if i < 0 {
		return ""
	}
	h := rawURL[i:]
	if len(h) < 2 || len(h) > 200 || h[1] != '/' {
		return ""
	}
	for _, c := range h[1:] {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("/-_.", c)) {
			return ""
		}
	}
	return h
}
