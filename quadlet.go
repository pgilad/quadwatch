package main

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	"gopkg.in/ini.v1"
)

var (
	quadletImageKeys  = [][2]string{{"Container", "Image"}, {"Image", "Image"}, {"Volume", "Image"}}
	quadletExtensions = map[string]struct{}{
		".container": {},
		".image":     {},
		".volume":    {},
	}
	ignoredTransports = []string{"dir:", "docker-archive:", "oci-archive:", "oci:", "containers-storage:", "sif:"}
)

func scanImages(root string) ([]Image, error) {
	var images []Image
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !isQuadletFile(path) {
			return nil
		}
		found, err := parseQuadlet(path)
		if err != nil {
			return fmt.Errorf("parse quadlet %s: %w", path, err)
		}
		images = append(images, found...)
		return nil
	})
	return images, err
}

func isQuadletFile(path string) bool {
	_, ok := quadletExtensions[filepath.Ext(path)]
	return ok
}

func parseQuadlet(path string) ([]Image, error) {
	cfg, err := ini.LoadSources(ini.LoadOptions{Insensitive: true}, path)
	if err != nil {
		return nil, err
	}
	var out []Image
	for _, p := range quadletImageKeys {
		sec := cfg.Section(p[0])
		if sec == nil || !sec.HasKey(p[1]) {
			continue
		}
		raw := strings.TrimSpace(sec.Key(p[1]).String())
		img, ok := normalizeImage(path, raw)
		if ok {
			out = append(out, img)
		}
	}
	return out, nil
}

func normalizeImage(file, raw string) (Image, bool) {
	if raw == "" {
		return Image{}, false
	}
	for _, prefix := range ignoredTransports {
		if strings.HasPrefix(raw, prefix) {
			return Image{}, false
		}
	}
	raw = strings.TrimPrefix(raw, "docker://")
	raw = strings.TrimPrefix(raw, "docker-daemon:")
	ref, err := name.ParseReference(raw, name.WithDefaultRegistry("docker.io"), name.WithDefaultTag("latest"))
	if err != nil {
		return Image{}, false
	}
	repo := ref.Context().Name()
	tag := "latest"
	digest := ""
	if t, ok := ref.(name.Tag); ok {
		tag = t.TagStr()
	}
	if d, ok := ref.(name.Digest); ok {
		digest = d.DigestStr()
		tag = tagBeforeDigest(raw)
	}
	return Image{File: file, Image: raw, Repository: repo, Tag: tag, Digest: digest}, true
}

func tagBeforeDigest(raw string) string {
	namePart, _, _ := strings.Cut(raw, "@")
	lastSlash := strings.LastIndex(namePart, "/")
	lastColon := strings.LastIndex(namePart, ":")
	if lastColon > lastSlash {
		return namePart[lastColon+1:]
	}
	return ""
}
