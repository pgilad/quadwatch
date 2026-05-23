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

func newestCompatible(current string, tags []string) (string, error) {
	cm := versionRe.FindStringSubmatch(current)
	if cm == nil {
		return "", errUnsupportedTagShape
	}
	curVer, err := hcversion.NewVersion(cm[2])
	if err != nil {
		return "", err
	}
	prefix, suffix := cm[1], cm[3]
	currentSegments := strings.Count(cm[2], ".") + 1
	var newestTag string
	var newestVersion *hcversion.Version
	for _, tag := range tags {
		m := versionRe.FindStringSubmatch(tag)
		if m == nil || m[1] != prefix || m[3] != suffix {
			continue
		}
		candidateSegments := strings.Count(m[2], ".") + 1
		if candidateSegments < currentSegments {
			continue
		}
		if prefix != "" && prefix != "v" && candidateSegments != currentSegments {
			continue
		}
		v, err := hcversion.NewVersion(m[2])
		if err != nil {
			continue
		}
		if !v.GreaterThan(curVer) || (newestVersion != nil && !v.GreaterThan(newestVersion)) {
			continue
		}
		newestTag = tag
		newestVersion = v
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
