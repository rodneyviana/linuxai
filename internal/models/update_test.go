package models

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestUpdateFromWritesAndSkipsUnchanged(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(fixtureSource))
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "models.json")
	first, err := UpdateFrom(context.Background(), server.URL, path)
	if err != nil {
		t.Fatalf("UpdateFrom: %v", err)
	}
	if !first.Changed || first.Count != 4 {
		t.Fatalf("first update = %+v, want changed with 4 profiles", first)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("catalog mode = %o, want 600", mode)
	}

	second, err := UpdateFrom(context.Background(), server.URL, path)
	if err != nil {
		t.Fatalf("second UpdateFrom: %v", err)
	}
	if second.Changed {
		t.Error("re-running with identical source should not rewrite the catalog")
	}

	saved, fromDisk, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !fromDisk {
		t.Error("saved catalog should load from disk")
	}
	if saved.SHA256 == "" {
		t.Error("saved catalog is missing its source checksum")
	}
	if _, present := saved.Profiles["vendor/chat-model"]; !present {
		t.Error("saved catalog is missing the parsed profile")
	}
}

func TestFetchRejectsBadResponses(t *testing.T) {
	notFound := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer notFound.Close()
	if _, err := Fetch(context.Background(), notFound.URL); err == nil {
		t.Error("expected an error for a non-200 response")
	}

	garbage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not python"))
	}))
	defer garbage.Close()
	if _, err := Fetch(context.Background(), garbage.URL); err == nil {
		t.Error("expected an error for unparseable content")
	}

	if _, err := Fetch(context.Background(), "ftp://example.com/x.py"); err == nil {
		t.Error("expected an error for an unsupported scheme")
	}
}

func TestFetchAvailable(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/v1/models" {
			http.Error(w, "wrong path "+r.URL.Path, http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"id": "vendor/b"}, {"id": "vendor/a"}, {"id": ""}},
		})
	}))
	defer server.Close()

	ids, err := FetchAvailable(context.Background(), server.URL+"/v1/", "secret")
	if err != nil {
		t.Fatalf("FetchAvailable: %v", err)
	}
	if len(ids) != 2 || ids[0] != "vendor/a" || ids[1] != "vendor/b" {
		t.Errorf("ids = %v, want sorted vendor/a, vendor/b", ids)
	}
	if gotAuth != "Bearer secret" {
		t.Errorf("Authorization = %q", gotAuth)
	}
}

func TestFetchAvailableReportsFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "denied", http.StatusUnauthorized)
	}))
	defer server.Close()
	if _, err := FetchAvailable(context.Background(), server.URL+"/v1", ""); err == nil {
		t.Error("expected an error for a 401 response")
	}
}
