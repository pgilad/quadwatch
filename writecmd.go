package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
)

type pinResult struct {
	digest string
	err    error
}

func runUpdate(args []string) error {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	configPath := fs.String("config", "", "YAML config file")
	dryRun := fs.Bool("dry-run", false, "show planned file changes without writing them")
	pin := fs.Bool("pin", false, "pin updated images to their top-level digest")
	if err := fs.Parse(args); err != nil {
		return err
	}
	path, err := pathArg(fs)
	if err != nil {
		return err
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	pinEnabled := *pin || cfg.AlwaysPin
	// Only update-specific options can enable pinning. Fetch configuration must
	// not silently enable pinning for a write operation.
	cfg.Fetch.CheckDigestPinned = pinEnabled
	cfg.Fetch.ResolveDigests = pinEnabled

	scan, err := scanImageFiles(path)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	updates := fetchUpdates(ctx, scan.Images, cfg, false, colors{})
	if err := updateLookupErrors(updates); err != nil {
		return err
	}
	edits, err := buildUpdateEdits(scan.Images, updates, pinEnabled)
	if err != nil {
		return err
	}
	return applyEdits(scan, edits, *dryRun, editActionUpdate, os.Stdout)
}

func runPin(args []string) error {
	fs := flag.NewFlagSet("pin", flag.ExitOnError)
	dryRun := fs.Bool("dry-run", false, "show planned file changes without writing them")
	if err := fs.Parse(args); err != nil {
		return err
	}
	path, err := pathArg(fs)
	if err != nil {
		return err
	}
	scan, err := scanImageFiles(path)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	results := resolveCurrentDigests(ctx, scan.Images)
	if err := pinLookupErrors(scan.Images, results); err != nil {
		return err
	}
	edits, err := buildPinEdits(scan.Images, results)
	if err != nil {
		return err
	}
	return applyEdits(scan, edits, *dryRun, editActionPin, os.Stdout)
}

func buildUpdateEdits(images []Image, updates []Update, pin bool) ([]Edit, error) {
	if len(images) != len(updates) {
		return nil, fmt.Errorf("image and update counts do not match")
	}
	edits := make([]Edit, 0)
	for i, update := range updates {
		if !update.Update {
			continue
		}
		digest := ""
		if pin {
			digest = update.NewestDigest
			if digest == "" {
				return nil, fmt.Errorf("no digest was resolved for %s:%s", update.Repository, update.NewestTag)
			}
		}
		newReference, err := rewriteImageReference(images[i], update.NewestTag, digest)
		if err != nil {
			return nil, err
		}
		if newReference == images[i].Source.Original {
			continue
		}
		edits = append(edits, imageEdit(images[i], newReference))
	}
	return edits, nil
}

func buildPinEdits(images []Image, results []pinResult) ([]Edit, error) {
	if len(images) != len(results) {
		return nil, fmt.Errorf("image and digest counts do not match")
	}
	edits := make([]Edit, 0)
	for i, img := range images {
		if img.Digest != "" {
			continue
		}
		if results[i].digest == "" {
			return nil, fmt.Errorf("no digest was resolved for %s:%s", img.Repository, img.Tag)
		}
		newReference, err := rewriteImageReference(img, img.Tag, results[i].digest)
		if err != nil {
			return nil, err
		}
		edits = append(edits, imageEdit(img, newReference))
	}
	return edits, nil
}

func imageEdit(img Image, newReference string) Edit {
	return Edit{
		File:  img.File,
		Start: img.Source.Start,
		End:   img.Source.End,
		Old:   img.Source.Original,
		New:   newReference,
	}
}

func resolveCurrentDigests(ctx context.Context, images []Image) []pinResult {
	results := make([]pinResult, len(images))
	fetcher := &updateFetcher{registryDigestCache: map[string]registryDigestResult{}}
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxConcurrentRemoteLookups)
	for i, img := range images {
		if img.Digest != "" {
			continue
		}
		i, img := i, img
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			digest, err := fetcher.registryDigest(ctx, img.Repository, img.Tag)
			if err != nil {
				err = fmt.Errorf("resolve digest for %s:%s: %w", img.Repository, img.Tag, err)
			}
			results[i] = pinResult{digest: digest, err: err}
		}()
	}
	wg.Wait()
	return results
}

func updateLookupErrors(updates []Update) error {
	errors := make([]string, 0)
	for _, update := range updates {
		if update.Error != "" {
			errors = append(errors, fmt.Sprintf("%s:%s: %s", update.Repository, update.CurrentTag, update.Error))
		}
	}
	return lookupError(errors)
}

func pinLookupErrors(images []Image, results []pinResult) error {
	errors := make([]string, 0)
	for i, result := range results {
		if result.err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", images[i].File, result.err))
		}
	}
	return lookupError(errors)
}

func lookupError(errors []string) error {
	if len(errors) == 0 {
		return nil
	}
	return fmt.Errorf("%d image lookup(s) failed: %s", len(errors), strings.Join(errors, "; "))
}
