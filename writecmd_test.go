package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

func TestRunUpdatePinChangesTagAndTopLevelDigest(t *testing.T) {
	repository, closeRegistry := testRegistry(t)
	defer closeRegistry()
	oldIndex := writeRandomIndex(t, repository, "1.2.0")
	newIndex := writeRandomIndex(t, repository, "1.3.0")
	oldDigest, err := oldIndex.Digest()
	if err != nil {
		t.Fatal(err)
	}
	newDigest, err := newIndex.Digest()
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	path := filepath.Join(dir, "app.container")
	original := fmt.Sprintf("# keep\n[cOnTaInEr]\n  iMaGe = \"docker://%s:1.2.0@%s\" ; keep\n\n[Unit]\nDescription=Keep me\n", repository, oldDigest)
	if err := os.WriteFile(path, []byte(original), 0o640); err != nil {
		t.Fatal(err)
	}

	output := captureStdout(t, func() {
		if err := runUpdate([]string{"--pin", dir}); err != nil {
			t.Fatal(err)
		}
	})
	want := fmt.Sprintf("# keep\n[cOnTaInEr]\n  iMaGe = \"docker://%s:1.3.0@%s\" ; keep\n\n[Unit]\nDescription=Keep me\n", repository, newDigest)
	assertFileContents(t, path, want)
	if !strings.Contains(output, "Updated 1 image in 1 file:") || !strings.Contains(output, path+":") || !strings.Contains(output, "tag     1.2.0 → 1.3.0") || !strings.Contains(output, "digest  sha256:") {
		t.Fatalf("runUpdate() output = %q", output)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("updated mode = %o, want 640", info.Mode().Perm())
	}
}

func TestRunUpdateDoesNotUseFetchDigestConfiguration(t *testing.T) {
	repository, closeRegistry := testRegistry(t)
	defer closeRegistry()
	writeRandomImage(t, repository, "1.2.0")
	writeRandomImage(t, repository, "1.3.0")

	dir := t.TempDir()
	path := filepath.Join(dir, "app.container")
	if err := os.WriteFile(path, []byte(fmt.Sprintf("[Container]\nImage=%s:1.2.0\n", repository)), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "quadwatch.yaml")
	if err := os.WriteFile(configPath, []byte("fetch:\n  check_digest_pinned: true\n  resolve_digests: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runUpdate([]string{"--config", configPath, dir}); err != nil {
		t.Fatal(err)
	}
	assertFileContents(t, path, fmt.Sprintf("[Container]\nImage=%s:1.3.0\n", repository))
}

func TestRunUpdateAlwaysPinConfigChangesTagAndDigest(t *testing.T) {
	repository, closeRegistry := testRegistry(t)
	defer closeRegistry()
	oldIndex := writeRandomIndex(t, repository, "1.2.0")
	newIndex := writeRandomIndex(t, repository, "1.3.0")
	oldDigest, err := oldIndex.Digest()
	if err != nil {
		t.Fatal(err)
	}
	newDigest, err := newIndex.Digest()
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "app.container")
	if err := os.WriteFile(path, []byte(fmt.Sprintf("[Container]\nImage=%s:1.2.0@%s\n", repository, oldDigest)), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "quadwatch.yaml")
	if err := os.WriteFile(configPath, []byte("always_pin: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runUpdate([]string{"--config", configPath, dir}); err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("[Container]\nImage=%s:1.3.0@%s\n", repository, newDigest)
	assertFileContents(t, path, want)
}

func TestRunUpdateSkipsPinnedImageWithoutPinFlag(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "app.container")
	original := "[Container]\nImage=example.invalid/app:1.2.0@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runUpdate([]string{dir}); err != nil {
		t.Fatal(err)
	}
	assertFileContents(t, path, original)
}

func TestRunPinSkipsExistingPins(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.container")
	original := "[Container]\nImage=example.invalid/app:1.2.0@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n\n[Image]\nImage=example.invalid/sidecar@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runPin([]string{dir}); err != nil {
		t.Fatal(err)
	}
	assertFileContents(t, path, original)
}

func TestRunPinMakesLatestExplicitAndUsesTopLevelDigest(t *testing.T) {
	repository, closeRegistry := testRegistry(t)
	defer closeRegistry()
	index := writeRandomIndex(t, repository, "latest")
	digest, err := index.Digest()
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "app.container")
	if err := os.WriteFile(path, []byte(fmt.Sprintf("[Container]\nImage=docker://%s # keep\n", repository)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runPin([]string{dir}); err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("[Container]\nImage=docker://%s:latest@%s # keep\n", repository, digest)
	assertFileContents(t, path, want)
}

func TestRunPinDryRunDoesNotWrite(t *testing.T) {
	repository, closeRegistry := testRegistry(t)
	defer closeRegistry()
	writeRandomImage(t, repository, "1.2.0")

	dir := t.TempDir()
	path := filepath.Join(dir, "app.container")
	original := fmt.Sprintf("[Container]\nImage=%s:1.2.0\n", repository)
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	output := captureStdout(t, func() {
		if err := runPin([]string{"--dry-run", dir}); err != nil {
			t.Fatal(err)
		}
	})
	assertFileContents(t, path, original)
	if !strings.Contains(output, "Would pin 1 image in 1 file:") || !strings.Contains(output, path+":") || !strings.Contains(output, "digest  unpinned → sha256:") {
		t.Fatalf("runPin() dry-run output = %q", output)
	}
}

func TestRunPinLookupFailureChangesNoFiles(t *testing.T) {
	repository, closeRegistry := testRegistry(t)
	defer closeRegistry()
	writeRandomImage(t, repository, "1.2.0")
	failingRegistry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer failingRegistry.Close()
	failingRepository := strings.TrimPrefix(failingRegistry.URL, "http://") + "/example/missing"

	dir := t.TempDir()
	path := filepath.Join(dir, "app.container")
	original := fmt.Sprintf("[Container]\nImage=%s:1.2.0\n\n[Image]\nImage=%s:1.2.0\n", repository, failingRepository)
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	err := runPin([]string{dir})
	if err == nil || !strings.Contains(err.Error(), "image lookup(s) failed") {
		t.Fatalf("runPin() error = %v", err)
	}
	assertFileContents(t, path, original)
}

func testRegistry(t *testing.T) (string, func()) {
	t.Helper()
	server := httptest.NewServer(registry.New())
	repository := strings.TrimPrefix(server.URL, "http://") + "/example/app"
	return repository, server.Close
}

func writeRandomImage(t *testing.T, repository, tag string) v1.Image {
	t.Helper()
	image, err := random.Image(256, 1)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := name.NewTag(repository+":"+tag, name.Insecure)
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.Write(ref, image); err != nil {
		t.Fatal(err)
	}
	return image
}

func writeRandomIndex(t *testing.T, repository, tag string) v1.ImageIndex {
	t.Helper()
	index, err := random.Index(256, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := name.NewTag(repository+":"+tag, name.Insecure)
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.WriteIndex(ref, index); err != nil {
		t.Fatal(err)
	}
	return index
}

func assertFileContents(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("file contents = %q, want %q", got, want)
	}
}
