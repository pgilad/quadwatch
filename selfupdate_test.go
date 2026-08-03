package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLatestReleaseTagWithClient(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/releases/latest" {
			t.Errorf("request path = %q, want /releases/latest", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"tag_name":"2026.08.03-80"}`)
	}))
	defer server.Close()

	got, err := latestReleaseTagWithClient(server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if got != "2026.08.03-80" {
		t.Fatalf("latestReleaseTagWithClient() = %q, want 2026.08.03-80", got)
	}
}

func TestDownloadInstallerWithClientUsesVersionedReleaseAsset(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/2026.08.03-80/install.sh" {
			t.Errorf("request path = %q, want /2026.08.03-80/install.sh", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		_, _ = fmt.Fprint(w, "#!/usr/bin/env sh\n")
	}))
	defer server.Close()

	got, err := downloadInstallerWithClient(server.Client(), server.URL, "2026.08.03-80")
	if err != nil {
		t.Fatal(err)
	}
	if got != "#!/usr/bin/env sh\n" {
		t.Fatalf("downloadInstallerWithClient() = %q", got)
	}
}

func TestDownloadInstallerWithClientRejectsMissingAsset(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	if _, err := downloadInstallerWithClient(server.Client(), server.URL, "missing"); err == nil {
		t.Fatal("downloadInstallerWithClient() succeeded for a missing installer")
	}
}
