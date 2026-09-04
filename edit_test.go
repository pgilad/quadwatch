package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseQuadletRecordsExactImageSource(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.container")
	data := []byte("# header\r\n[cOnTaInEr]\r\n  iMaGe  =  \"docker://ghcr.io/example/app:1.2.0\"  ; keep\r\n\r\n[Unit]\r\nDescription=Keep me\r\n")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	images, err := parseQuadlet(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 1 {
		t.Fatalf("parseQuadlet() found %d images, want 1", len(images))
	}
	img := images[0]
	if img.Source.Original != "docker://ghcr.io/example/app:1.2.0" {
		t.Fatalf("source original = %q", img.Source.Original)
	}
	if got := string(data[img.Source.Start:img.Source.End]); got != img.Source.Original {
		t.Fatalf("source range = %q, want %q", got, img.Source.Original)
	}
	if img.Image != "ghcr.io/example/app:1.2.0" || img.Tag != "1.2.0" {
		t.Fatalf("parsed image = %#v", img)
	}
}

func TestRewriteImageReference(t *testing.T) {
	t.Parallel()

	digest := "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	tests := []struct {
		name     string
		original string
		tag      string
		digest   string
		want     string
	}{
		{
			name:     "preserve transport and replace tag",
			original: "docker://ghcr.io/example/app:1.2.0",
			tag:      "1.3.0",
			want:     "docker://ghcr.io/example/app:1.3.0",
		},
		{
			name:     "make implicit latest explicit",
			original: "postgres",
			tag:      "latest",
			digest:   digest,
			want:     "postgres:latest@" + digest,
		},
		{
			name:     "replace tag and existing digest",
			original: "registry.example:5000/app:1.2.0@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			tag:      "1.3.0",
			digest:   digest,
			want:     "registry.example:5000/app:1.3.0@" + digest,
		},
		{
			name:     "preserve docker daemon transport",
			original: "docker-daemon:example/app:1.2.0",
			tag:      "1.3.0",
			want:     "docker-daemon:example/app:1.3.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			img := Image{File: "app.container", Image: tt.original, Source: ImageSource{Original: tt.original}}
			got, err := rewriteImageReference(img, tt.tag, tt.digest)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("rewriteImageReference() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOutputEditsFormatsReadableSummary(t *testing.T) {
	t.Parallel()

	edits := []Edit{
		{
			File: "roles/cooper_quadlets/files/quadlets/paperless-ngx/paperless.container",
			Old:  "docker.io/paperlessngx/paperless-ngx:3.1.2@sha256:5ab4f4f9bb099a36bec3e092906ea3e611323c5f18dc5cc38c76a1d540bdca9c",
			New:  "docker.io/paperlessngx/paperless-ngx:3.1.3@sha256:aa810a36942c63d4ee70d00eda7236cd3d6acfb7eb3f7987fb568ed14df8817a",
		},
		{
			File: "roles/cooper_quadlets/files/quadlets/sure/sure-web.container",
			Old:  "ghcr.io/we-promise/sure:0.7.5-alpha.2@sha256:c4043ecc77b9d32cc8f0b602d1785f1fbc26a50587c3fd9f67e51ff9228fedfe",
			New:  "ghcr.io/we-promise/sure:0.7.5-alpha.3@sha256:8a88efcdcd9c08af08010563ec58a42cc4e8f9e56fcaebc2fd4e149a2f09716f",
		},
		{
			File: "roles/cooper_quadlets/files/quadlets/sure/sure-worker.container",
			Old:  "ghcr.io/we-promise/sure:0.7.5-alpha.2@sha256:c4043ecc77b9d32cc8f0b602d1785f1fbc26a50587c3fd9f67e51ff9228fedfe",
			New:  "ghcr.io/we-promise/sure:0.7.5-alpha.3@sha256:8a88efcdcd9c08af08010563ec58a42cc4e8f9e56fcaebc2fd4e149a2f09716f",
		},
	}
	var output bytes.Buffer
	if err := outputEdits(&output, edits, false, editActionUpdate); err != nil {
		t.Fatal(err)
	}
	want := `Updated 3 images in 3 files:

roles/cooper_quadlets/files/quadlets/paperless-ngx/paperless.container:
  docker.io/paperlessngx/paperless-ngx
    tag     3.1.2 → 3.1.3
    digest  sha256:5ab4f4f9bb09… → sha256:aa810a36942c…

roles/cooper_quadlets/files/quadlets/sure/sure-web.container:
  ghcr.io/we-promise/sure
    tag     0.7.5-alpha.2 → 0.7.5-alpha.3
    digest  sha256:c4043ecc77b9… → sha256:8a88efcdcd9c…

roles/cooper_quadlets/files/quadlets/sure/sure-worker.container:
  ghcr.io/we-promise/sure
    tag     0.7.5-alpha.2 → 0.7.5-alpha.3
    digest  sha256:c4043ecc77b9… → sha256:8a88efcdcd9c…
`
	if output.String() != want {
		t.Fatalf("outputEdits() output:\n%s\nwant:\n%s", output.String(), want)
	}
}

func TestOutputEditsReportsNoChanges(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	if err := outputEdits(&output, nil, false, editActionUpdate); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "No changes.\n"; got != want {
		t.Fatalf("outputEdits() = %q, want %q", got, want)
	}
}

func TestApplyEditsPreservesContentAndPermissions(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "app.container")
	original := []byte("# keep\n[Container]\nImage = example/app:1.2.0 # keep\n\n[Image]\nImage=example/sidecar:2.0.0\n")
	if err := os.WriteFile(path, original, 0o640); err != nil {
		t.Fatal(err)
	}
	scan, err := scanImageFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(scan.Images) != 2 {
		t.Fatalf("scan found %d images, want 2", len(scan.Images))
	}
	edits := []Edit{
		imageEdit(scan.Images[0], "example/app:1.3.0"),
		imageEdit(scan.Images[1], "example/sidecar:2.1.0"),
	}
	var dryRun bytes.Buffer
	if err := applyEdits(scan, edits, true, editActionUpdate, &dryRun); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dryRun.String(), "Would update 2 images in 1 file:") || !strings.Contains(dryRun.String(), path+":\n  example/app\n    tag     1.2.0 → 1.3.0") {
		t.Fatalf("dry-run output = %q", dryRun.String())
	}
	if got, err := os.ReadFile(path); err != nil {
		t.Fatal(err)
	} else if !bytes.Equal(got, original) {
		t.Fatalf("dry-run changed file to %q", got)
	}

	if err := applyEdits(scan, edits, false, editActionUpdate, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	want := "# keep\n[Container]\nImage = example/app:1.3.0 # keep\n\n[Image]\nImage=example/sidecar:2.1.0\n"
	if got, err := os.ReadFile(path); err != nil {
		t.Fatal(err)
	} else if string(got) != want {
		t.Fatalf("updated file = %q, want %q", got, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("updated mode = %o, want 640", info.Mode().Perm())
	}
}

func TestApplyEditsRejectsFileChangedAfterScan(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "app.container")
	if err := os.WriteFile(path, []byte("[Container]\nImage=example/app:1.2.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	scan, err := scanImageFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[Container]\nImage=example/app:1.2.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = applyEdits(scan, []Edit{imageEdit(scan.Images[0], "example/app:1.3.0")}, false, editActionUpdate, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "changed after it was scanned") {
		t.Fatalf("applyEdits() error = %v", err)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "[Container]\nImage=example/app:1.2.1\n" {
		t.Fatalf("applyEdits() changed modified file to %q", got)
	}
}
