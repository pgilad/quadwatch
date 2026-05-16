package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/hashicorp/go-version"
	"gopkg.in/ini.v1"
)

type Image struct {
	File       string `json:"file"`
	Image      string `json:"image"`
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
}

type Update struct {
	File       string `json:"file"`
	Image      string `json:"image"`
	Repository string `json:"repository"`
	CurrentTag string `json:"currentTag"`
	NewestTag  string `json:"newestTag"`
	Update     bool   `json:"update"`
	Error      string `json:"error,omitempty"`
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	cmd := os.Args[1]
	switch cmd {
	case "images":
		if err := runImages(os.Args[2:]); err != nil {
			fatal(err)
		}
	case "fetch":
		if err := runFetch(os.Args[2:]); err != nil {
			fatal(err)
		}
	case "-h", "--help", "help":
		usage()
	default:
		fatal(fmt.Errorf("unknown command: %s", cmd))
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `quadlet-updates finds container image updates in Quadlet files.

Usage:
  quadlet-updates images [--format json|csv|table] PATH
  quadlet-updates fetch  [--format json|csv|table] PATH

Commands:
  images  List detected images and current tags
  fetch   List detected images and newest compatible remote tags
`)
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}

func runImages(args []string) error {
	fs := flag.NewFlagSet("images", flag.ExitOnError)
	format := fs.String("format", "table", "output format: json, csv, table")
	if err := fs.Parse(args); err != nil {
		return err
	}
	path := "."
	if fs.NArg() > 0 {
		path = fs.Arg(0)
	}
	images, err := scanImages(path)
	if err != nil {
		return err
	}
	return outputImages(images, *format)
}

func runFetch(args []string) error {
	fs := flag.NewFlagSet("fetch", flag.ExitOnError)
	format := fs.String("format", "table", "output format: json, csv, table")
	if err := fs.Parse(args); err != nil {
		return err
	}
	path := "."
	if fs.NArg() > 0 {
		path = fs.Arg(0)
	}
	images, err := scanImages(path)
	if err != nil {
		return err
	}
	updates := fetchUpdates(context.Background(), images)
	return outputUpdates(updates, *format)
}

func scanImages(root string) ([]Image, error) {
	var images []Image
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := filepath.Ext(path)
		if ext != ".container" && ext != ".image" && ext != ".volume" {
			return nil
		}
		found, err := parseQuadlet(path)
		if err != nil {
			return nil
		}
		images = append(images, found...)
		return nil
	})
	return images, err
}

func parseQuadlet(path string) ([]Image, error) {
	cfg, err := ini.LoadSources(ini.LoadOptions{Insensitive: true}, path)
	if err != nil {
		return nil, err
	}
	pairs := [][2]string{{"Container", "Image"}, {"Image", "Image"}, {"Volume", "Image"}}
	var out []Image
	for _, p := range pairs {
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
	ignored := []string{"dir:", "docker-archive:", "oci-archive:", "oci:", "containers-storage:", "sif:"}
	for _, prefix := range ignored {
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
	if t, ok := ref.(name.Tag); ok {
		tag = t.TagStr()
	}
	return Image{File: file, Image: raw, Repository: repo, Tag: tag}, true
}

func fetchUpdates(ctx context.Context, images []Image) []Update {
	updates := make([]Update, len(images))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, img := range images {
		i, img := i, img
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			updates[i] = fetchOne(ctx, img)
		}()
	}
	wg.Wait()
	return updates
}

func fetchOne(ctx context.Context, img Image) Update {
	u := Update{File: img.File, Image: img.Image, Repository: img.Repository, CurrentTag: img.Tag}
	repo, err := name.NewRepository(img.Repository)
	if err != nil {
		u.Error = err.Error()
		return u
	}
	tags, err := remote.List(repo, remote.WithContext(ctx), remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		u.Error = err.Error()
		return u
	}
	newest, err := newestCompatible(img.Tag, tags)
	if err != nil {
		u.Error = err.Error()
		return u
	}
	u.NewestTag = newest
	u.Update = newest != "" && newest != img.Tag
	return u
}

var versionRe = regexp.MustCompile(`^(v?)([0-9]+(?:\.[0-9]+){0,3})(.*)$`)

func newestCompatible(current string, tags []string) (string, error) {
	cm := versionRe.FindStringSubmatch(current)
	if cm == nil {
		return "", errors.New("current tag is not version-like")
	}
	curVer, err := version.NewVersion(cm[2])
	if err != nil {
		return "", err
	}
	prefix, suffix := cm[1], cm[3]
	type candidate struct {
		tag string
		ver *version.Version
	}
	var candidates []candidate
	for _, tag := range tags {
		m := versionRe.FindStringSubmatch(tag)
		if m == nil || m[1] != prefix || m[3] != suffix {
			continue
		}
		v, err := version.NewVersion(m[2])
		if err != nil {
			continue
		}
		if v.GreaterThanOrEqual(curVer) {
			candidates = append(candidates, candidate{tag, v})
		}
	}
	if len(candidates) == 0 {
		return "", nil
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].ver.GreaterThan(candidates[j].ver) })
	return candidates[0].tag, nil
}

func outputImages(images []Image, format string) error {
	switch format {
	case "json":
		return json.NewEncoder(os.Stdout).Encode(images)
	case "csv":
		w := csv.NewWriter(os.Stdout)
		defer w.Flush()
		_ = w.Write([]string{"quadlet", "image_name", "current_tag"})
		for _, i := range images {
			_ = w.Write([]string{i.File, i.Repository, i.Tag})
		}
		return w.Error()
	case "table":
		fmt.Printf("%-60s  %-45s  %s\n", "QUADLET", "IMAGE", "TAG")
		for _, i := range images {
			fmt.Printf("%-60s  %-45s  %s\n", i.File, i.Repository, i.Tag)
		}
		return nil
	default:
		return fmt.Errorf("unknown format: %s", format)
	}
}

func outputUpdates(updates []Update, format string) error {
	switch format {
	case "json":
		return json.NewEncoder(os.Stdout).Encode(updates)
	case "csv":
		w := csv.NewWriter(os.Stdout)
		defer w.Flush()
		_ = w.Write([]string{"quadlet", "image_name", "current_tag", "newest_tag", "update", "error"})
		for _, u := range updates {
			_ = w.Write([]string{u.File, u.Repository, u.CurrentTag, u.NewestTag, fmt.Sprint(u.Update), u.Error})
		}
		return w.Error()
	case "table":
		fmt.Printf("%-60s  %-45s  %-18s  %-18s  %s\n", "QUADLET", "IMAGE", "CURRENT", "NEWEST", "ERROR")
		for _, u := range updates {
			fmt.Printf("%-60s  %-45s  %-18s  %-18s  %s\n", u.File, u.Repository, u.CurrentTag, u.NewestTag, u.Error)
		}
		return nil
	default:
		return fmt.Errorf("unknown format: %s", format)
	}
}
