package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestShowRunning(t *testing.T) {
	hit := ""
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = r.Method + " " + r.URL.Path + " " + r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ok.Close()
	if !showRunning(strings.TrimPrefix(ok.URL, "http://")) {
		t.Fatal("204: vreau true")
	}
	if hit != "POST /api/app/show application/json" {
		t.Fatalf("cerere: %q", hit)
	}

	old := httptest.NewServer(http.NotFoundHandler()) // un HobStack vechi, fara /api/app/show
	defer old.Close()
	if showRunning(strings.TrimPrefix(old.URL, "http://")) {
		t.Fatal("404: vreau false")
	}

	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	if showRunning(addr) {
		t.Fatal("port inchis: vreau false")
	}
}

func TestRestartArgs(t *testing.T) {
	got := restartArgs([]string{"start", "--port", "7791", "--show-window"}, false)
	if strings.Join(got, " ") != "start --port 7791 --no-open" {
		t.Fatalf("fara fereastra: %v", got)
	}
	got = restartArgs([]string{"start", "-no-open"}, true)
	if strings.Join(got, " ") != "start -no-open --show-window" {
		t.Fatalf("cu fereastra: %v", got)
	}
	got = restartArgs([]string{"start", "-show-window", "--no-open"}, true)
	if strings.Join(got, " ") != "start --no-open --show-window" {
		t.Fatalf("o singura data: %v", got)
	}
}
