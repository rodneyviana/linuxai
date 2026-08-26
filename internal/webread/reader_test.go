package webread

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTrustedURL(t *testing.T) {
	for _, rawURL := range []string{
		"https://kernel.org/",
		"https://docs.kernel.org/admin-guide/",
		"https://en.wikipedia.org/wiki/Linux",
		"https://man7.org/linux/man-pages/",
	} {
		if !TrustedURL(rawURL) {
			t.Errorf("TrustedURL(%q) = false", rawURL)
		}
	}
	for _, rawURL := range []string{
		"https://kernel.org.example.com/",
		"https://users.kernel.org/",
		"https://kernel.org:8443/",
		"http://kernel.org/",
		"https://example.com/",
		"file:///etc/passwd",
	} {
		if TrustedURL(rawURL) {
			t.Errorf("TrustedURL(%q) = true", rawURL)
		}
	}
}

func TestReadRequiresAuthorizationBeforeRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("must not be fetched"))
	}))
	defer server.Close()

	reader := New(func(string, string) bool { return false })
	reader.allowPrivate = true
	if _, err := reader.Read(context.Background(), server.URL); err == nil || !strings.Contains(err.Error(), "not authorized") {
		t.Fatalf("Read error = %v, want authorization denial", err)
	}
	if requests != 0 {
		t.Errorf("requests = %d, want zero before authorization", requests)
	}
}

func TestReadExtractsHTMLAndSkipsUnsafeElements(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<html><head><title>Kernel Notes</title><script>ignore()</script></head><body><nav>menu</nav><main><h1>Linux 7</h1><p>Current release notes.</p><a href="/Articles/123456/">Full article</a></main></body></html>`))
	}))
	defer server.Close()

	reader := New(func(string, string) bool { return true })
	reader.allowPrivate = true
	page, err := reader.Read(context.Background(), server.URL+"/notes")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if page.Title != "Kernel Notes" || page.Content != "Linux 7 Current release notes. Full article" {
		t.Errorf("page = %+v", page)
	}
	if strings.Contains(page.Content, "ignore") || strings.Contains(page.Content, "menu") {
		t.Errorf("unsafe or navigation text leaked into content: %q", page.Content)
	}
	if len(page.Links) != 1 || page.Links[0].Text != "Full article" || page.Links[0].URL != server.URL+"/Articles/123456/" {
		t.Errorf("links = %+v", page.Links)
	}
}

func TestReadClassifiesArticleHTML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><head><meta property="og:type" content="article"><title>Article</title></head><body><article><p>Full body.</p></article></body></html>`))
	}))
	defer server.Close()

	reader := New(func(string, string) bool { return true })
	reader.allowPrivate = true
	page, err := reader.Read(context.Background(), server.URL+"/Articles/123456/")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if page.Kind != "article" || page.Content != "Full body." {
		t.Errorf("page = %+v", page)
	}
}

func TestReadRejectsPrivateAddressAfterAuthorization(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()

	reader := New(func(string, string) bool { return true })
	_, err := reader.Read(context.Background(), server.URL)
	if err == nil || !strings.Contains(err.Error(), "non-public address") {
		t.Fatalf("Read error = %v, want non-public address rejection", err)
	}
}

func TestReadReauthorizesCrossOriginRedirect(t *testing.T) {
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("destination"))
	}))
	defer destination.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusFound)
	}))
	defer source.Close()

	var origins []string
	reader := New(func(origin, _ string) bool {
		origins = append(origins, origin)
		return len(origins) == 1
	})
	reader.allowPrivate = true
	_, err := reader.Read(context.Background(), source.URL)
	if err == nil || !strings.Contains(err.Error(), "not authorized") {
		t.Fatalf("Read error = %v, want redirected-origin denial", err)
	}
	if len(origins) != 2 || origins[0] == origins[1] {
		t.Errorf("authorized origins = %v, want two distinct origins", origins)
	}
}

func TestReadRejectsBinaryContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write([]byte("binary"))
	}))
	defer server.Close()

	reader := New(func(string, string) bool { return true })
	reader.allowPrivate = true
	_, err := reader.Read(context.Background(), server.URL)
	if err == nil || !strings.Contains(err.Error(), "unsupported content type") {
		t.Fatalf("Read error = %v, want content-type rejection", err)
	}
}

func TestReadExtractsRSSArticleLinks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
		w.Write([]byte(`<?xml version="1.0"?><rss><channel><title>LWN.net</title><item><title>Kernel development</title><link>https://lwn.net/Articles/123456/</link><pubDate>Tue, 25 Aug 2026</pubDate><description><![CDATA[<p>The full article summary.</p>]]></description></item></channel></rss>`))
	}))
	defer server.Close()

	reader := New(func(string, string) bool { return true })
	reader.allowPrivate = true
	page, err := reader.Read(context.Background(), server.URL+"/Articles/rss")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if page.Kind != "feed" || page.Title != "LWN.net" {
		t.Errorf("page metadata = %+v", page)
	}
	for _, want := range []string{"Kernel development", "https://lwn.net/Articles/123456/", "The full article summary."} {
		if !strings.Contains(page.Content, want) {
			t.Errorf("feed content missing %q: %q", want, page.Content)
		}
	}
}
