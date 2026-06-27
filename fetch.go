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
	"golang.org/x/sync/singleflight"
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
		registryTagCache:   map[string]registryTagResult{},
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

	registryTagMu    sync.Mutex
	registryTagCache map[string]registryTagResult
	registryTagGroup singleflight.Group
}

type githubReleaseResult struct {
	tags []string
	err  error
}

type registryTagResult struct {
	tags []string
	err  error
}

func (f *updateFetcher) fetchOne(ctx context.Context, img Image) Update {
	repoConfig := f.config.repositoryConfig(img.Repository)
	if repoConfig.GitHubRelease != "" {
		return f.fetchOneGitHubReleaseWithConfig(ctx, img, repoConfig)
	}
	return f.fetchOneRegistryWithConfig(ctx, img, repoConfig)
}

func newUpdate(img Image) Update {
	return Update{File: img.File, Image: img.Image, Repository: img.Repository, CurrentTag: img.Tag}
}

func skipUnsupportedCurrentTag(img Image, opts compatibilityOptions) (Update, bool) {
	u := newUpdate(img)
	if img.Digest != "" {
		u.SkipReason = "digest-pinned image"
		return u, true
	}
	_, err := newestCompatibleWithOptions(img.Tag, nil, opts)
	if err != nil {
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
	return f.fetchOneGitHubReleaseWithConfig(ctx, img, RepositoryConfig{GitHubRelease: githubRepo})
}

func (f *updateFetcher) fetchOneGitHubReleaseWithConfig(ctx context.Context, img Image, repoConfig RepositoryConfig) Update {
	opts := compatibilityOptions{includePrereleases: repoConfig.IncludePrereleases}
	u, skipped := skipUnsupportedCurrentTag(img, opts)
	if skipped {
		return u
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	tags, err := f.githubReleaseTags(ctx, repoConfig.GitHubRelease, repoConfig.IncludePrereleases)
	if err != nil {
		u.Error = err.Error()
		return u
	}
	newest, err := newestCompatibleWithOptions(img.Tag, tags, opts)
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

func (f *updateFetcher) githubReleaseTags(ctx context.Context, githubRepo string, includePrereleases bool) ([]string, error) {
	cacheKey := fmt.Sprintf("%t\x00%s", includePrereleases, githubRepo)

	f.githubReleaseMu.Lock()
	defer f.githubReleaseMu.Unlock()

	if cached, ok := f.githubReleaseCache[cacheKey]; ok {
		return cached.tags, cached.err
	}

	tags, err := fetchGitHubReleaseTags(ctx, githubRepo, includePrereleases)
	result := githubReleaseResult{tags: tags, err: err}
	f.githubReleaseCache[cacheKey] = result
	return result.tags, result.err
}

func fetchGitHubReleaseTags(ctx context.Context, githubRepo string, includePrereleases bool) ([]string, error) {
	if includePrereleases {
		return fetchGitHubReleaseList(ctx, githubRepo)
	}
	return fetchLatestGitHubRelease(ctx, githubRepo)
}

func fetchLatestGitHubRelease(ctx context.Context, githubRepo string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "gh", "release", "view", "-R", githubRepo, "--json", "tagName", "-q", ".tagName")
	tags, err := runGitHubReleaseTagCommand(cmd, fmt.Sprintf("gh release view -R %s", githubRepo))
	if err != nil {
		return nil, err
	}
	return tags, nil
}

func fetchGitHubReleaseList(ctx context.Context, githubRepo string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "gh", "release", "list", "-R", githubRepo, "--exclude-drafts", "--limit", "100", "--json", "tagName", "-q", ".[].tagName")
	tags, err := runGitHubReleaseTagCommand(cmd, fmt.Sprintf("gh release list -R %s", githubRepo))
	if err != nil {
		return nil, err
	}
	return tags, nil
}

func runGitHubReleaseTagCommand(cmd *exec.Cmd, description string) ([]string, error) {
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return nil, fmt.Errorf("%s: %w: %s", description, err, detail)
		}
		return nil, fmt.Errorf("%s: %w", description, err)
	}
	tags := nonEmptyLines(string(out))
	if len(tags) == 0 {
		return nil, fmt.Errorf("%s returned no tags", description)
	}
	return tags, nil
}

func nonEmptyLines(output string) []string {
	lines := strings.Split(output, "\n")
	tags := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			tags = append(tags, line)
		}
	}
	return tags
}

func fetchOneRegistry(ctx context.Context, img Image) Update {
	fetcher := &updateFetcher{registryTagCache: map[string]registryTagResult{}}
	return fetcher.fetchOneRegistry(ctx, img)
}

func (f *updateFetcher) fetchOneRegistry(ctx context.Context, img Image) Update {
	return f.fetchOneRegistryWithConfig(ctx, img, RepositoryConfig{})
}

func (f *updateFetcher) fetchOneRegistryWithConfig(ctx context.Context, img Image, repoConfig RepositoryConfig) Update {
	opts := compatibilityOptions{includePrereleases: repoConfig.IncludePrereleases}
	u, skipped := skipUnsupportedCurrentTag(img, opts)
	if skipped {
		return u
	}
	tags, err := f.registryTags(ctx, img.Repository)
	if err != nil {
		u.Error = err.Error()
		return u
	}
	newest, err := newestCompatibleWithOptions(img.Tag, tags, opts)
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

func (f *updateFetcher) registryTags(ctx context.Context, repository string) ([]string, error) {
	f.registryTagMu.Lock()
	if cached, ok := f.registryTagCache[repository]; ok {
		f.registryTagMu.Unlock()
		return cached.tags, cached.err
	}
	f.registryTagMu.Unlock()

	result, _, _ := f.registryTagGroup.Do(repository, func() (any, error) {
		f.registryTagMu.Lock()
		if cached, ok := f.registryTagCache[repository]; ok {
			f.registryTagMu.Unlock()
			return cached, nil
		}
		f.registryTagMu.Unlock()

		ctx, cancel := context.WithTimeout(ctx, requestTimeout)
		defer cancel()
		repo, err := name.NewRepository(repository)
		var tags []string
		if err == nil {
			tags, err = remote.List(repo, remote.WithContext(ctx), remote.WithAuthFromKeychain(authn.DefaultKeychain))
			if tags != nil {
				tags = append([]string(nil), tags...)
			}
		}
		cached := registryTagResult{tags: tags, err: err}

		f.registryTagMu.Lock()
		f.registryTagCache[repository] = cached
		f.registryTagMu.Unlock()
		return cached, nil
	})

	cached := result.(registryTagResult)
	return cached.tags, cached.err
}
