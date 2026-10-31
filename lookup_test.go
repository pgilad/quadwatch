package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandsWithFailedLookups(t *testing.T) {
	commands := []struct {
		name     string
		run      func([]string) error
		args     []string
		fetch    bool
		pin      bool
		newerTag bool
	}{
		{name: "update", run: runUpdate, newerTag: true},
		{name: "update pin", run: runUpdate, args: []string{"--pin"}, pin: true, newerTag: true},
		{name: "pin", run: runPin, pin: true},
		{name: "fetch", run: runFetch, args: []string{"--format", "json"}, fetch: true, newerTag: true},
		{name: "fetch resolve digests", run: runFetch, args: []string{"--format", "json", "--resolve-digests"}, fetch: true, pin: true, newerTag: true},
	}
	for _, command := range commands {
		for _, failure := range []string{"tags", "digest"} {
			if failure == "digest" && !command.pin {
				continue
			}
			for _, mode := range []string{"default", "break-on-error", "dry-run"} {
				if command.fetch && mode == "dry-run" {
					continue
				}
				t.Run(command.name+"/"+failure+"/"+mode, func(t *testing.T) {
					t.Setenv("XDG_CONFIG_HOME", t.TempDir())
					repository, closeRegistry := testRegistry(t)
					defer closeRegistry()
					currentImage := writeRandomImage(t, repository, "1.2.0")
					newImage := writeRandomImage(t, repository, "1.3.0")
					selectedImage := currentImage
					if command.newerTag {
						selectedImage = newImage
					}
					digest, err := selectedImage.Digest()
					if err != nil {
						t.Fatal(err)
					}

					failingRegistry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if failure == "digest" {
							switch r.URL.Path {
							case "/v2/":
								_, _ = fmt.Fprint(w, "{}")
								return
							case "/v2/example/private/tags/list":
								w.Header().Set("Content-Type", "application/json")
								_, _ = fmt.Fprint(w, `{"name":"example/private","tags":["1.2.0","1.3.0"]}`)
								return
							}
						}
						w.WriteHeader(http.StatusUnauthorized)
					}))
					defer failingRegistry.Close()
					failingRepository := strings.TrimPrefix(failingRegistry.URL, "http://") + "/example/private"

					dir := t.TempDir()
					path := filepath.Join(dir, "app.container")
					failedSection := fmt.Sprintf("[Container]\nImage=%s:1.2.0\n\n", failingRepository)
					original := failedSection + fmt.Sprintf("[Image]\nImage=%s:1.2.0\n", repository)
					if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
						t.Fatal(err)
					}
					args := append([]string{}, command.args...)
					if mode != "default" {
						args = append(args, "--"+mode)
					}
					args = append(args, dir)
					var runErr error
					var stdout string
					stderr := captureStderr(t, func() {
						stdout = captureStdout(t, func() {
							runErr = command.run(args)
						})
					})
					if mode == "break-on-error" {
						if runErr == nil || !strings.Contains(runErr.Error(), "1 image lookup(s) failed") {
							t.Fatalf("command error = %v, want lookup error", runErr)
						}
						if strings.Contains(stderr, "warning:") {
							t.Fatalf("strict command printed warning: %s", stderr)
						}
					} else {
						if runErr != nil {
							t.Fatal(runErr)
						}
						if !strings.Contains(stderr, "warning:") || !strings.Contains(stderr, failingRepository) || !strings.Contains(stderr, "401") {
							t.Fatalf("missing failed lookup warning: %s", stderr)
						}
					}
					want := original
					if !command.fetch && mode == "default" {
						newReference := repository + ":1.2.0"
						if command.newerTag {
							newReference = repository + ":1.3.0"
						}
						if command.pin {
							newReference += "@" + digest.String()
						}
						want = failedSection + fmt.Sprintf("[Image]\nImage=%s\n", newReference)
					}
					assertFileContents(t, path, want)
					if command.fetch {
						var updates []Update
						if err := json.Unmarshal([]byte(stdout), &updates); err != nil {
							t.Fatalf("invalid JSON output: %v\n%s", err, stdout)
						}
						if len(updates) != 2 || updates[0].Error == "" || !updates[1].Update || updates[1].NewestTag != "1.3.0" {
							t.Fatalf("fetch results = %#v", updates)
						}
						if command.pin && updates[1].NewestDigest != digest.String() {
							t.Fatalf("fetch digest = %q, want %q", updates[1].NewestDigest, digest)
						}
					} else if mode == "dry-run" && !strings.Contains(stdout, "Would ") {
						t.Fatalf("missing planned changes: %s", stdout)
					}
				})
			}
		}
	}
}

func TestCommandsWithOnlyFailedLookups(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	repository := strings.TrimPrefix(server.URL, "http://") + "/example/private"
	dir := t.TempDir()
	path := filepath.Join(dir, "app.container")
	original := fmt.Sprintf("[Container]\nImage=%s:1.2.0\n", repository)
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, run := range []struct {
		name string
		fn   func([]string) error
	}{
		{name: "update", fn: runUpdate},
		{name: "pin", fn: runPin},
		{name: "fetch", fn: runFetch},
	} {
		t.Run(run.name, func(t *testing.T) {
			stderr := captureStderr(t, func() {
				captureStdout(t, func() {
					if err := run.fn([]string{dir}); err != nil {
						t.Fatal(err)
					}
				})
			})
			if !strings.Contains(stderr, "warning:") || !strings.Contains(stderr, repository) {
				t.Fatalf("missing lookup warning: %s", stderr)
			}
			assertFileContents(t, path, original)
		})
	}
}
