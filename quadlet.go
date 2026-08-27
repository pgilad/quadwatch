package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	"gopkg.in/ini.v1"
)

type scannedFile struct {
	Contents []byte
	Mode     fs.FileMode
}

type imageScan struct {
	Images []Image
	Files  map[string]scannedFile
}

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
	scan, err := scanImageFiles(root)
	return scan.Images, err
}

func scanImageFiles(root string) (imageScan, error) {
	scan := imageScan{Files: map[string]scannedFile{}}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !isQuadletFile(path) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read quadlet %s: %w", path, err)
		}
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("stat quadlet %s: %w", path, err)
		}
		found, err := parseQuadletData(path, data)
		if err != nil {
			return fmt.Errorf("parse quadlet %s: %w", path, err)
		}
		scan.Images = append(scan.Images, found...)
		scan.Files[path] = scannedFile{Contents: data, Mode: info.Mode()}
		return nil
	})
	return scan, err
}

func isQuadletFile(path string) bool {
	_, ok := quadletExtensions[filepath.Ext(path)]
	return ok
}

func parseQuadlet(path string) ([]Image, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseQuadletData(path, data)
}

func parseQuadletData(path string, data []byte) ([]Image, error) {
	cfg, err := ini.LoadSources(ini.LoadOptions{Insensitive: true}, data)
	if err != nil {
		return nil, err
	}
	sources := findImageSources(data)
	var out []Image
	for _, p := range quadletImageKeys {
		sec := cfg.Section(p[0])
		if sec == nil || !sec.HasKey(p[1]) {
			continue
		}
		raw := strings.TrimSpace(sec.Key(p[1]).String())
		img, ok := normalizeImage(path, raw)
		if ok {
			source, found := sources[strings.ToLower(p[0])]
			if !found {
				return nil, fmt.Errorf("find source for [%s] %s", p[0], p[1])
			}
			img.Source = source
			out = append(out, img)
		}
	}
	return out, nil
}

func findImageSources(data []byte) map[string]ImageSource {
	sources := map[string]ImageSource{}
	section := ""
	for offset := 0; offset < len(data); {
		lineEnd := bytes.IndexByte(data[offset:], '\n')
		if lineEnd < 0 {
			lineEnd = len(data)
		} else {
			lineEnd += offset
		}
		line := bytes.TrimSpace(data[offset:lineEnd])
		if len(line) != 0 && line[0] != '#' && line[0] != ';' {
			if line[0] == '[' {
				if closeIndex := bytes.LastIndexByte(line, ']'); closeIndex >= 0 {
					section = strings.ToLower(string(line[1:closeIndex]))
				}
			} else if isSupportedImageSection(section) {
				delimiter := bytes.IndexAny(line, "=:")
				if delimiter >= 0 && strings.EqualFold(strings.TrimSpace(string(line[:delimiter])), "Image") {
					lineOffset := offset + bytes.Index(data[offset:lineEnd], line)
					start, end := imageValueRange(line, delimiter+1)
					sources[section] = ImageSource{
						Start:    lineOffset + start,
						End:      lineOffset + end,
						Original: string(line[start:end]),
					}
				}
			}
		}
		if lineEnd == len(data) {
			break
		}
		offset = lineEnd + 1
	}
	return sources
}

func isSupportedImageSection(section string) bool {
	for _, pair := range quadletImageKeys {
		if strings.EqualFold(pair[0], section) {
			return true
		}
	}
	return false
}

func imageValueRange(line []byte, valueOffset int) (int, int) {
	value := bytes.TrimSpace(line[valueOffset:])
	if len(value) == 0 {
		return valueOffset, valueOffset
	}
	start := valueOffset + bytes.Index(line[valueOffset:], value)

	if value[0] == '`' {
		if closeIndex := bytes.LastIndexByte(value[1:], '`'); closeIndex >= 0 {
			return start + 1, start + 1 + closeIndex
		}
	}
	if len(value) >= 6 && bytes.HasPrefix(value, []byte(`"""`)) {
		if closeIndex := bytes.LastIndex(value[3:], []byte(`"""`)); closeIndex >= 0 {
			return start + 3, start + 3 + closeIndex
		}
	}

	if commentIndex := bytes.IndexAny(value, "#;"); commentIndex >= 0 {
		value = bytes.TrimSpace(value[:commentIndex])
	}
	end := start + len(value)
	if len(value) >= 2 && (value[0] == '\'' || value[0] == '"') && value[len(value)-1] == value[0] && bytes.IndexByte(value[1:len(value)-1], value[0]) < 0 {
		return start + 1, end - 1
	}
	return start, end
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
