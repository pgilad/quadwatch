package main

import "testing"

func TestNewestCompatibleVersionFormatChange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		current            string
		tags               []string
		want               string
		includePrereleases bool
	}{
		{
			name:    "new major uses major minor releases",
			current: "10.11.11",
			tags:    []string{"10.11.11", "12", "12.0", "12.1", "12.4"},
			want:    "12.4",
		},
		{
			name:    "v prefix permits release format change",
			current: "v10.11.11",
			tags:    []string{"v12.0", "v12.4"},
			want:    "v12.4",
		},
		{
			name:    "matching flavor permits release format change",
			current: "10.11.11-alpine",
			tags:    []string{"12.4-alpine", "12.5-bookworm"},
			want:    "12.4-alpine",
		},
		{
			name:    "numeric image revision permits release format change",
			current: "10.11.11-2",
			tags:    []string{"12.4-1", "12.4-3"},
			want:    "12.4-3",
		},
		{
			name:    "same major keeps floating minor protection",
			current: "10.11.11",
			tags:    []string{"10.12", "10.11.12"},
			want:    "10.11.12",
		},
		{
			name:    "new major keeps floating major protection",
			current: "10.11.11",
			tags:    []string{"12"},
		},
		{
			name:    "new major with patch releases keeps floating minor protection",
			current: "10.11.11",
			tags:    []string{"12.4", "12.3.1"},
			want:    "12.3.1",
		},
		{
			name:    "floating minor detection does not depend on tag order",
			current: "10.11.11",
			tags:    []string{"12.3.1", "12.4"},
			want:    "12.3.1",
		},
		{
			name:    "full release wins over its floating alias",
			current: "10.11.11",
			tags:    []string{"12.4", "12.4.0"},
			want:    "12.4.0",
		},
		{
			name:    "other major does not block release format change",
			current: "10.11.11",
			tags:    []string{"11.0.0", "12.4"},
			want:    "12.4",
		},
		{
			name:    "other flavor does not block release format change",
			current: "10.11.11-alpine",
			tags:    []string{"12.4-alpine", "12.3.1-bookworm"},
			want:    "12.4-alpine",
		},
		{
			name:    "other prefix does not block release format change",
			current: "10.11.11",
			tags:    []string{"12.4", "v12.3.1"},
			want:    "12.4",
		},
		{
			name:    "custom prefix keeps exact segment count",
			current: "release-10.11.11",
			tags:    []string{"release-12.4", "release-12.0.1"},
			want:    "release-12.0.1",
		},
		{
			name:    "custom prefix cannot shorten version",
			current: "release-10.11.11",
			tags:    []string{"release-12.4"},
		},
		{
			name:    "build version cannot shorten version",
			current: "10.11.11.1",
			tags:    []string{"12.4", "12.4.1"},
		},
		{
			name:    "major minor release cannot change to major alias",
			current: "12.4",
			tags:    []string{"13"},
		},
		{
			name:    "older major cannot shorten version",
			current: "12.4.1",
			tags:    []string{"10.12"},
		},
		{
			name:               "prerelease opt in permits release format change",
			current:            "10.11.11-rc.1",
			tags:               []string{"12.4-rc.1", "12.4"},
			want:               "12.4",
			includePrereleases: true,
		},
		{
			name:               "prerelease opt in keeps floating minor protection",
			current:            "10.11.11",
			tags:               []string{"12.4", "12.3.1-rc.1"},
			want:               "12.3.1-rc.1",
			includePrereleases: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := newestCompatibleWithOptions(tt.current, tt.tags, compatibilityOptions{includePrereleases: tt.includePrereleases})
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("newestCompatibleWithOptions(%q, %v) = %q, want %q", tt.current, tt.tags, got, tt.want)
			}
		})
	}
}

func TestNewestCompatibleJellyfinBuildTags(t *testing.T) {
	t.Parallel()

	got, err := newestCompatible("10.11.11", []string{
		"10.11.11", "12", "12.0", "12.4", "12.4.20261026-211555",
		"12.4-amd64.20261026-211555", "12.4-arm64.20261026-211555", "unstable", "latest",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "12.4" {
		t.Fatalf("newestCompatible() = %q, want 12.4", got)
	}
}
