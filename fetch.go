package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

const (
	maxConcurrentRemoteLookups = 8
	requestTimeout             = 30 * time.Second
)

func fetchUpdates(ctx context.Context, images []Image, cfg Config, progress bool, progressColors colors) []Update {
	updates := make([]Update, len(images))
	fetcher := &updateFetcher{
		config:             cfg,
		githubReleaseCache: map[string]githubReleaseResult{},
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxConcurrentRemoteLookups)
	progressCh := make(chan Update)
	progressDone := make(chan struct{})

	if progress {
		go func() {
			reportProgress(len(images), progressCh, progressColors)
			close(progressDone)
		}()
	}

	for i, img := range images {
		i, img := i, img
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			started := time.Now()
			update := fetcher.fetchOne(ctx, img)
			update.Duration = time.Since(started)
			updates[i] = update
			if progress {
				progressCh <- update
			}
		}()
	}
	wg.Wait()
	if progress {
		close(progressCh)
		<-progressDone
	}
	return updates
}

func reportProgress(total int, updates <-chan Update, colors colors) {
	completed := 0
	for update := range updates {
		completed++
		status := updateStatus(update)
		newest := colors.dim(update.NewestTag)
		if update.Error != "" {
			newest = colors.red(update.NewestTag)
		} else if update.Update {
			newest = colors.brightGreen(update.NewestTag)
		}
		fmt.Fprintf(
			os.Stderr,
			"%s %s:%s %s %s (%s, %s)\n",
			colors.dim(fmt.Sprintf("[%d/%d]", completed, total)),
			colors.cyan(update.Repository),
			colors.yellow(update.CurrentTag),
			colors.dim("->"),
			newest,
			colors.status(status),
			colors.dim(fmt.Sprintf("%dms", update.Duration.Milliseconds())),
		)
	}
}

type updateFetcher struct {
	config Config

	githubReleaseMu    sync.Mutex
	githubReleaseCache map[string]githubReleaseResult
}

type githubReleaseResult struct {
	tag string
	err error
}

func (f *updateFetcher) fetchOne(ctx context.Context, img Image) Update {
	if githubRepo, ok := f.config.GitHubReleases[img.Repository]; ok {
		return f.fetchOneGitHubRelease(ctx, img, githubRepo)
	}
	return fetchOneRegistry(ctx, img)
}

func newUpdate(img Image) Update {
	return Update{File: img.File, Image: img.Image, Repository: img.Repository, CurrentTag: img.Tag}
}

func skipUnsupportedCurrentTag(img Image) (Update, bool) {
	u := newUpdate(img)
	if _, err := parseVersionTag(img.Tag); err != nil {
		if errors.Is(err, errUnsupportedTagShape) {
			u.SkipReason = err.Error()
		} else {
			u.Error = err.Error()
		}
		return u, true
	}
	return u, false
}

func (f *updateFetcher) fetchOneGitHubRelease(ctx context.Context, img Image, githubRepo string) Update {
	u, skipped := skipUnsupportedCurrentTag(img)
	if skipped {
		return u
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	tag, err := f.latestGitHubRelease(ctx, githubRepo)
	if err != nil {
		u.Error = err.Error()
		return u
	}
	newest, err := newestCompatible(img.Tag, []string{tag})
	if err != nil {
		if errors.Is(err, errUnsupportedTagShape) {
			u.SkipReason = err.Error()
		} else {
			u.Error = err.Error()
		}
		return u
	}
	u.NewestTag = newest
	u.Update = newest != "" && newest != img.Tag
	return u
}

func (f *updateFetcher) latestGitHubRelease(ctx context.Context, githubRepo string) (string, error) {
	f.githubReleaseMu.Lock()
	defer f.githubReleaseMu.Unlock()

	if cached, ok := f.githubReleaseCache[githubRepo]; ok {
		return cached.tag, cached.err
	}

	cmd := exec.CommandContext(ctx, "gh", "release", "view", "-R", githubRepo, "--json", "tagName", "-q", ".tagName")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	tag := strings.TrimSpace(string(out))
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			err = fmt.Errorf("gh release view -R %s: %w: %s", githubRepo, err, detail)
		} else {
			err = fmt.Errorf("gh release view -R %s: %w", githubRepo, err)
		}
	} else if tag == "" {
		err = fmt.Errorf("gh release view -R %s returned empty tag", githubRepo)
	}

	result := githubReleaseResult{tag: tag, err: err}
	f.githubReleaseCache[githubRepo] = result
	return result.tag, result.err
}

func fetchOneRegistry(ctx context.Context, img Image) Update {
	u, skipped := skipUnsupportedCurrentTag(img)
	if skipped {
		return u
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
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
		if errors.Is(err, errUnsupportedTagShape) {
			u.SkipReason = err.Error()
		} else {
			u.Error = err.Error()
		}
		return u
	}
	u.NewestTag = newest
	u.Update = newest != "" && newest != img.Tag
	return u
}
