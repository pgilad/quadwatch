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
	prereleaseVersionRe    = regexp.MustCompile(`^([^0-9]*?)([0-9]+(?:\.[0-9]+){0,3}(?:-[0-9A-Za-z][0-9A-Za-z.-]*)?(?:\+[0-9A-Za-z][0-9A-Za-z.-]*)?)$`)
	numericRevisionRe      = regexp.MustCompile(`^-[0-9]+$`)
)

type compatibilityOptions struct {
	includePrereleases bool
}

type parsedVersionTag struct {
	prefix   string
	version  *hcversion.Version
	suffix   string
	segments int
	revision bool
}

func parseVersionTag(tag string) (parsedVersionTag, error) {
	m := versionRe.FindStringSubmatch(tag)
	if m == nil {
		return parsedVersionTag{}, errUnsupportedTagShape
	}
	version := m[2]
	revision := numericRevisionRe.MatchString(m[3])
	if revision {
		version += m[3]
	}
	v, err := hcversion.NewVersion(version)
	if err != nil {
		return parsedVersionTag{}, err
	}
	return parsedVersionTag{
		prefix:   m[1],
		version:  v,
		suffix:   m[3],
		segments: strings.Count(m[2], ".") + 1,
		revision: revision,
	}, nil
}

func parsePrereleaseVersionTag(tag string) (parsedVersionTag, error) {
	m := prereleaseVersionRe.FindStringSubmatch(tag)
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
		segments: versionSegmentCount(m[2]),
	}, nil
}

func versionSegmentCount(version string) int {
	core := version
	if before, _, ok := strings.Cut(core, "-"); ok {
		core = before
	}
	if before, _, ok := strings.Cut(core, "+"); ok {
		core = before
	}
	return strings.Count(core, ".") + 1
}

func newestCompatible(current string, tags []string) (string, error) {
	return newestCompatibleWithOptions(current, tags, compatibilityOptions{})
}

func newestCompatibleWithOptions(current string, tags []string, opts compatibilityOptions) (string, error) {
	if opts.includePrereleases {
		return newestCompatiblePrerelease(current, tags)
	}
	return newestCompatibleDefault(current, tags)
}

func newestCompatibleDefault(current string, tags []string) (string, error) {
	currentTag, err := parseVersionTag(current)
	if err != nil {
		return "", err
	}
	return newestParsedCompatible(currentTag, tags, parseVersionTag, false)
}

func newestCompatiblePrerelease(current string, tags []string) (string, error) {
	currentTag, err := parsePrereleaseVersionTag(current)
	if err != nil {
		return "", err
	}
	return newestParsedCompatible(currentTag, tags, parsePrereleaseVersionTag, true)
}

func newestParsedCompatible(currentTag parsedVersionTag, tags []string, parse func(string) (parsedVersionTag, error), allowPrereleaseSuffixChanges bool) (string, error) {
	var newestTag string
	var newestVersion *hcversion.Version
	for _, tag := range tags {
		candidate, err := parse(tag)
		if err != nil || candidate.prefix != currentTag.prefix {
			continue
		}
		if !allowPrereleaseSuffixChanges && !compatibleSuffix(currentTag, candidate) {
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

func compatibleSuffix(current, candidate parsedVersionTag) bool {
	if current.revision || candidate.revision {
		return current.revision && candidate.revision
	}
	return candidate.suffix == current.suffix
}

func updatesWithAvailableUpdatesOrErrors(updates []Update) []Update {
	filtered := updates[:0]
	for _, u := range updates {
		if u.Update || u.Error != "" {
			filtered = append(filtered, u)
		}
	}
	return filtered
}

func updateErrorCount(updates []Update) int {
	count := 0
	for _, u := range updates {
		if u.Error != "" {
			count++
		}
	}
	return count
}
