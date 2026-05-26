package main

import (
	"errors"
	"regexp"
	"strings"

	hcversion "github.com/hashicorp/go-version"
)

var (
	errUnsupportedTagShape = errors.New("unsupported tag shape")
	versionRe              = regexp.MustCompile(`^([^0-9]*?)([0-9]+(?:\.[0-9]+){0,3})(.*)$`)
)

type parsedVersionTag struct {
	prefix   string
	version  *hcversion.Version
	suffix   string
	segments int
}

func parseVersionTag(tag string) (parsedVersionTag, error) {
	m := versionRe.FindStringSubmatch(tag)
	if m == nil {
		return parsedVersionTag{}, errUnsupportedTagShape
	}
	v, err := hcversion.NewVersion(m[2])
	if err != nil {
		return parsedVersionTag{}, err
	}
	return parsedVersionTag{
		prefix:   m[1],
		version:  v,
		suffix:   m[3],
		segments: strings.Count(m[2], ".") + 1,
	}, nil
}

func newestCompatible(current string, tags []string) (string, error) {
	currentTag, err := parseVersionTag(current)
	if err != nil {
		return "", err
	}
	var newestTag string
	var newestVersion *hcversion.Version
	for _, tag := range tags {
		candidate, err := parseVersionTag(tag)
		if err != nil || candidate.prefix != currentTag.prefix || candidate.suffix != currentTag.suffix {
			continue
		}
		if candidate.segments < currentTag.segments {
			continue
		}
		if currentTag.prefix != "" && currentTag.prefix != "v" && candidate.segments != currentTag.segments {
			continue
		}
		if !candidate.version.GreaterThan(currentTag.version) || (newestVersion != nil && !candidate.version.GreaterThan(newestVersion)) {
			continue
		}
		newestTag = tag
		newestVersion = candidate.version
	}
	return newestTag, nil
}

func updatesWithAvailableUpdates(updates []Update) []Update {
	filtered := updates[:0]
	for _, u := range updates {
		if u.Update {
			filtered = append(filtered, u)
		}
	}
	return filtered
}
