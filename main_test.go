package main

import (
	"encoding/csv"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewestCompatible(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		current string
		tags    []string
		want    string
		wantErr bool
	}{
		{
			name:    "semver",
			current: "1.2.3",
			tags:    []string{"1.2.2", "1.2.3", "1.3.0", "2.0.0-alpine"},
			want:    "1.3.0",
		},
		{
			name:    "v prefix",
			current: "v2.7.0",
			tags:    []string{"2.8.0", "v2.7.1", "v2.8.0-alpine"},
			want:    "v2.7.1",
		},
		{
			name:    "prefixed release keeps segment count",
			current: "release-4.0.16.2944",
			tags:    []string{"release-9831336", "release-4.0.17.2952", "nightly-4.0.18.1"},
			want:    "release-4.0.17.2952",
		},
		{
			name:    "suffix must match",
			current: "1.2.3-alpine",
			tags:    []string{"1.2.4", "1.2.4-alpine", "1.3.0-bookworm"},
			want:    "1.2.4-alpine",
		},
		{
			name:    "normalized equal version is not newer",
			current: "8.32.0",
			tags:    []string{"8.31.0", "8.32", "8.32.0"},
			want:    "",
		},
		{
			name:    "less precise floating tag is not newer",
			current: "9.0.4-alpine",
			tags:    []string{"9.1-alpine", "9.0.5-alpine"},
			want:    "9.0.5-alpine",
		},
		{
			name:    "non version tag",
			current: "latest",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := newestCompatible(tt.current, tt.tags)
			if (err != nil) != tt.wantErr {
				t.Fatalf("newestCompatible() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != errUnsupportedTagShape {
				t.Fatalf("newestCompatible() error = %v, want unsupported tag shape", err)
			}
			if got != tt.want {
				t.Fatalf("newestCompatible() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestScanImagesReturnsParseErrors(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	badQuadlet := filepath.Join(dir, "bad.container")
	if err := os.WriteFile(badQuadlet, []byte("[Container\nImage=postgres:16\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := scanImages(dir)
	if err == nil {
		t.Fatal("scanImages() succeeded with malformed quadlet")
	}
	if got := err.Error(); !strings.Contains(got, "parse quadlet") || !strings.Contains(got, badQuadlet) {
		t.Fatalf("scanImages() error = %q, want parse error with path", got)
	}
}

func TestUnsupportedTagShapeIsSkippedWithoutGitHubLookup(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	fetcher := updateFetcher{githubReleaseCache: map[string]githubReleaseResult{}}
	update := fetcher.fetchOneGitHubRelease(t.Context(), Image{
		File:       "app.container",
		Image:      "example/app:latest",
		Repository: "example/app",
		Tag:        "latest",
	}, "owner/repo")
	if update.Error != "" {
		t.Fatalf("fetchOneGitHubRelease() error = %q", update.Error)
	}
	if update.SkipReason != "unsupported tag shape" {
		t.Fatalf("fetchOneGitHubRelease() skip reason = %q", update.SkipReason)
	}
	if update.Update {
		t.Fatal("fetchOneGitHubRelease() marked skipped image as update")
	}
	if got := updateStatus(update); got != "skipped" {
		t.Fatalf("updateStatus() = %q, want skipped", got)
	}
	if len(fetcher.githubReleaseCache) != 0 {
		t.Fatal("fetchOneGitHubRelease() looked up GitHub release for unsupported tag")
	}
}

func TestFetchOneRegistrySkipsUnsupportedTagBeforeRepositoryParsing(t *testing.T) {
	t.Parallel()

	update := fetchOneRegistry(t.Context(), Image{
		File:       "app.container",
		Image:      "not a valid repo:latest",
		Repository: "not a valid repo",
		Tag:        "latest",
	})
	if update.Error != "" {
		t.Fatalf("fetchOneRegistry() error = %q", update.Error)
	}
	if update.SkipReason != "unsupported tag shape" {
		t.Fatalf("fetchOneRegistry() skip reason = %q", update.SkipReason)
	}
	if update.Update {
		t.Fatal("fetchOneRegistry() marked skipped image as update")
	}
}

func TestNormalizeImage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		raw       string
		wantOK    bool
		wantImage string
		wantRepo  string
		wantTag   string
	}{
		{
			name:      "docker hub library default registry",
			raw:       "postgres:16",
			wantOK:    true,
			wantImage: "postgres:16",
			wantRepo:  "index.docker.io/library/postgres",
			wantTag:   "16",
		},
		{
			name:      "untagged image defaults to latest",
			raw:       "ghcr.io/owner/app",
			wantOK:    true,
			wantImage: "ghcr.io/owner/app",
			wantRepo:  "ghcr.io/owner/app",
			wantTag:   "latest",
		},
		{
			name:      "docker transport is stripped before parsing",
			raw:       "docker://ghcr.io/owner/app:v1.2.3",
			wantOK:    true,
			wantImage: "ghcr.io/owner/app:v1.2.3",
			wantRepo:  "ghcr.io/owner/app",
			wantTag:   "v1.2.3",
		},
		{
			name:   "ignored local transport",
			raw:    "oci:local",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			img, ok := normalizeImage("app.container", tt.raw)
			if ok != tt.wantOK {
				t.Fatalf("normalizeImage() ok = %v, want %v", ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if img.Image != tt.wantImage || img.Repository != tt.wantRepo || img.Tag != tt.wantTag {
				t.Fatalf("normalizeImage() = %#v", img)
			}
		})
	}
}

func TestParseQuadletExtractsSupportedSectionsAndSkipsLocalTransports(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.container")
	data := []byte(`
[Container]
Image=docker://ghcr.io/example/app:v1.2.3

[Image]
Image=quay.io/example/sidecar:2.0

[Volume]
Image=oci:local-volume
`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	images, err := parseQuadlet(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 2 {
		t.Fatalf("parseQuadlet() found %d images, want 2: %#v", len(images), images)
	}
	assertImage := func(index int, repo, tag string) {
		t.Helper()
		if images[index].File != path || images[index].Repository != repo || images[index].Tag != tag {
			t.Fatalf("image[%d] = %#v", index, images[index])
		}
	}
	assertImage(0, "ghcr.io/example/app", "v1.2.3")
	assertImage(1, "quay.io/example/sidecar", "2.0")
}

func TestScanImagesRecursesAndIgnoresUnsupportedFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	nested := filepath.Join(root, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ignored.service"), []byte("[Container]\nImage=postgres:16\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "app.image"), []byte("[Image]\nImage=ghcr.io/example/app:1.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	images, err := scanImages(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 1 {
		t.Fatalf("scanImages() found %d images, want 1: %#v", len(images), images)
	}
	if images[0].Repository != "ghcr.io/example/app" || images[0].Tag != "1.0.0" {
		t.Fatalf("scanImages() = %#v", images[0])
	}
}

func TestFetchOneGitHubReleaseUsesLatestReleaseTagCompatibility(t *testing.T) {
	binDir := t.TempDir()
	ghPath := filepath.Join(binDir, "gh")
	if err := os.WriteFile(ghPath, []byte("#!/bin/sh\nprintf '%s\\n' 'v1.3.0'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	fetcher := &updateFetcher{githubReleaseCache: map[string]githubReleaseResult{}}
	update := fetcher.fetchOneGitHubRelease(t.Context(), Image{
		File:       "app.container",
		Image:      "ghcr.io/example/app:v1.2.0",
		Repository: "ghcr.io/example/app",
		Tag:        "v1.2.0",
	}, "owner/repo")
	if update.Error != "" {
		t.Fatalf("fetchOneGitHubRelease() error = %q", update.Error)
	}
	if !update.Update || update.NewestTag != "v1.3.0" {
		t.Fatalf("fetchOneGitHubRelease() = %#v", update)
	}
}

func TestLoadConfigDefaultLookupOrder(t *testing.T) {
	tmp := t.TempDir()
	cwd := filepath.Join(tmp, "cwd")
	xdg := filepath.Join(tmp, "xdg")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(xdg, "quadwatch"), 0o755); err != nil {
		t.Fatal(err)
	}

	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldwd) })
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", xdg)

	xdgConfig := filepath.Join(xdg, "quadwatch", "config.yaml")
	if err := os.WriteFile(xdgConfig, []byte("github_releases:\n  ghcr.io/example/xdg: owner/xdg\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig("")
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.GitHubReleases["ghcr.io/example/xdg"]; got != "owner/xdg" {
		t.Fatalf("loadConfig() XDG entry = %q", got)
	}

	cwdConfig := filepath.Join(cwd, cwdConfigPath)
	if err := os.WriteFile(cwdConfig, []byte("github_releases:\n  ghcr.io/example/cwd: owner/cwd\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = loadConfig("")
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.GitHubReleases["ghcr.io/example/cwd"]; got != "owner/cwd" {
		t.Fatalf("loadConfig() cwd entry = %q", got)
	}
	if _, ok := cfg.GitHubReleases["ghcr.io/example/xdg"]; ok {
		t.Fatal("loadConfig() did not prefer cwd config over XDG config")
	}
}

func TestOutputUpdatesCSVIncludesSkipReason(t *testing.T) {
	output := captureStdout(t, func() {
		if err := outputUpdates([]Update{
			{
				File:       "app.container",
				Repository: "example/app",
				CurrentTag: "latest",
				SkipReason: "unsupported tag shape",
			},
		}, "csv", colors{}); err != nil {
			t.Fatal(err)
		}
	})

	records, err := csv.NewReader(strings.NewReader(output)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	wantHeader := []string{"quadlet", "image_name", "current_tag", "newest_tag", "update", "skip_reason", "error"}
	if strings.Join(records[0], ",") != strings.Join(wantHeader, ",") {
		t.Fatalf("CSV header = %#v, want %#v", records[0], wantHeader)
	}
	if got := records[1][5]; got != "unsupported tag shape" {
		t.Fatalf("CSV skip_reason = %q", got)
	}
}

func TestOutputUpdatesTableIncludesStatusAndDetails(t *testing.T) {
	output := captureStdout(t, func() {
		if err := outputUpdates([]Update{
			{
				File:       "app.container",
				Repository: "example/app",
				CurrentTag: "latest",
				SkipReason: "unsupported tag shape",
			},
		}, "table", colors{}); err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(output, "STATUS") || !strings.Contains(output, "DETAILS") {
		t.Fatalf("table output missing status/details columns:\n%s", output)
	}
	if !strings.Contains(output, "skipped") || !strings.Contains(output, "unsupported tag shape") {
		t.Fatalf("table output missing skipped details:\n%s", output)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = oldStdout }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestPathArg(t *testing.T) {
	t.Parallel()

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	if err := fs.Parse([]string{"/tmp/quadlets"}); err != nil {
		t.Fatal(err)
	}
	got, err := pathArg(fs)
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/quadlets" {
		t.Fatalf("pathArg() = %q", got)
	}

	fs = flag.NewFlagSet("test", flag.ContinueOnError)
	if err := fs.Parse([]string{"one", "two"}); err != nil {
		t.Fatal(err)
	}
	if _, err := pathArg(fs); err == nil {
		t.Fatal("pathArg() succeeded with too many arguments")
	}
}
