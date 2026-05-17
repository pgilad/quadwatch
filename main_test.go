package main

import (
	"flag"
	"os"
	"path/filepath"
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
			if got != tt.want {
				t.Fatalf("newestCompatible() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeImage(t *testing.T) {
	t.Parallel()

	img, ok := normalizeImage("app.container", "postgres:16")
	if !ok {
		t.Fatal("normalizeImage() did not accept registry image")
	}
	if img.Repository != "index.docker.io/library/postgres" || img.Tag != "16" {
		t.Fatalf("normalizeImage() = %#v", img)
	}

	if _, ok := normalizeImage("app.container", "oci:local"); ok {
		t.Fatal("normalizeImage() accepted ignored transport")
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
