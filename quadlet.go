package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
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
	quadletImageSections = []string{"container", "image", "volume"}
	quadletExtensions    = map[string]struct{}{
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
	values, err := findImageValues(data)
	if err != nil {
		return nil, err
	}
	var out []Image
	for _, section := range quadletImageSections {
		value := values[section]
		if section != "image" && (strings.HasSuffix(value.reference, ".image") || strings.HasSuffix(value.reference, ".build")) {
			continue
		}
		if img, ok := normalizeImage(path, value.reference); ok {
			img.Source = value.source
			out = append(out, img)
		}
	}
	return out, nil
}

type quadletImageValue struct {
	reference string
	source    ImageSource
}

type quadletLinePart struct {
	start  int
	offset int
	length int
}

func findImageValues(data []byte) (map[string]quadletImageValue, error) {
	values := make(map[string]quadletImageValue)
	section := ""
	var joined []byte
	var parts []quadletLinePart
	var quote []byte
	for offset, lineNumber := 0, 1; offset < len(data); lineNumber++ {
		physical, rest, _ := bytes.Cut(data[offset:], []byte{'\n'})
		lineStart := offset
		offset = len(data) - len(rest)
		if lineStart == 0 && bytes.HasPrefix(physical, []byte("\ufeff")) {
			physical = physical[3:]
			lineStart += 3
		}
		line := bytes.TrimSpace(physical)
		if len(quote) != 0 {
			if bytes.Contains(line, quote) {
				quote = nil
			}
			continue
		}
		if len(line) == 0 || line[0] == '#' || line[0] == ';' {
			continue
		}

		physicalOffset := lineStart + bytes.Index(physical, line)
		continued := line[len(line)-1] == '\\'
		if continued {
			line = line[:len(line)-1]
			continued = len(rest) != 0
		}
		if continued || len(parts) != 0 {
			parts = append(parts, quadletLinePart{start: len(joined), offset: physicalOffset, length: len(line)})
			joined = append(joined, line...)
			if continued {
				continue
			}
			line = joined
		} else {
			parts = append(parts, quadletLinePart{offset: physicalOffset, length: len(line)})
		}

		if len(line) == 0 {
			continue
		}
		if line[0] == '[' {
			closeIndex := bytes.LastIndexByte(line, ']')
			if closeIndex < 0 {
				return nil, fmt.Errorf("unterminated section on line %d", lineNumber)
			}
			section = strings.ToLower(strings.TrimSpace(string(line[1:closeIndex])))
		} else {
			delimiter := bytes.IndexAny(line, "=:")
			if delimiter < 0 {
				return nil, fmt.Errorf("invalid directive on line %d", lineNumber)
			}
			value := bytes.TrimSpace(line[delimiter+1:])
			for _, marker := range [][]byte{[]byte(`"""`), {'`'}} {
				if bytes.HasPrefix(value, marker) && !bytes.Contains(value[len(marker):], marker) {
					quote = marker
					break
				}
			}
			if strings.EqualFold(strings.TrimSpace(string(line[:delimiter])), "Image") {
				var image quadletImageValue
				if len(quote) == 0 {
					start, end := imageValueRange(line, delimiter+1)
					reference := bytes.TrimSpace(line[start:end])
					start += bytes.Index(line[start:end], reference)
					end = start + len(reference)
					image.reference = string(reference)
					// An edit must fit in one physical line to preserve the continuation.
					for _, part := range parts {
						if start >= part.start && end <= part.start+part.length {
							sourceStart := part.offset + start - part.start
							image.source = ImageSource{Start: sourceStart, End: sourceStart + end - start, Original: image.reference}
							break
						}
					}
				}
				values[section] = image
			}
		}
		joined = joined[:0]
		parts = parts[:0]
	}
	if len(quote) != 0 {
		return nil, fmt.Errorf("unterminated multiline value")
	}
	return values, nil
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
