package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/hashicorp/go-version"
	"gopkg.in/ini.v1"
	"gopkg.in/yaml.v3"
)

const maxConcurrentRemoteLookups = 8

const (
	colorAuto   = "auto"
	colorAlways = "always"
	colorNever  = "never"

	ansiReset        = "\x1b[0m"
	ansiDim          = "\x1b[2m"
	ansiRed          = "\x1b[31m"
	ansiGreen        = "\x1b[32m"
	ansiYellow       = "\x1b[33m"
	ansiCyan         = "\x1b[36m"
	ansiBrightGreen  = "\x1b[1;32m"
	ansiBrightYellow = "\x1b[1;33m"
	ansiBrightCyan   = "\x1b[1;36m"
)

const cwdConfigPath = "quadwatch.yaml"

var (
	quadletImageKeys  = [][2]string{{"Container", "Image"}, {"Image", "Image"}, {"Volume", "Image"}}
	quadletExtensions = map[string]struct{}{
		".container": {},
		".image":     {},
		".volume":    {},
	}
	ignoredTransports = []string{"dir:", "docker-archive:", "oci-archive:", "oci:", "containers-storage:", "sif:"}
)

type Config struct {
	GitHubReleases map[string]string `yaml:"github_releases"`
}

type Image struct {
	File       string `json:"file"`
	Image      string `json:"image"`
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
}

type Update struct {
	File       string        `json:"file"`
	Image      string        `json:"image"`
	Repository string        `json:"repository"`
	CurrentTag string        `json:"currentTag"`
	NewestTag  string        `json:"newestTag"`
	Update     bool          `json:"update"`
	Error      string        `json:"error,omitempty"`
	Duration   time.Duration `json:"-"`
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
	fmt.Fprintf(os.Stderr, `quadwatch finds container image updates in Quadlet files.

Usage:
  quadwatch images [--format json|csv|table] [--color auto|always|never] PATH
  quadwatch fetch    [--format json|csv|table] [--color auto|always|never] [--all] [--config PATH] [--progress] PATH

Commands:
  images  List detected images and current tags
  fetch   List images with updates and newest compatible remote tags

Options:
  --all          Show all images, including those without updates
  --color MODE   Colorize human-readable output: auto, always, never (default auto)
  --config PATH  YAML config file (defaults to ./quadwatch.yaml, then XDG config)
  --progress     Show remote lookup progress on stderr
`)
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}

func runImages(args []string) error {
	fs := flag.NewFlagSet("images", flag.ExitOnError)
	format := fs.String("format", "table", "output format: json, csv, table")
	colorMode := fs.String("color", colorAuto, "color output: auto, always, never")
	if err := fs.Parse(args); err != nil {
		return err
	}
	path, err := pathArg(fs)
	if err != nil {
		return err
	}
	images, err := scanImages(path)
	if err != nil {
		return err
	}
	colors, err := newColors(*colorMode, os.Stdout)
	if err != nil {
		return err
	}
	return outputImages(images, *format, colors)
}

func runFetch(args []string) error {
	fs := flag.NewFlagSet("fetch", flag.ExitOnError)
	format := fs.String("format", "table", "output format: json, csv, table")
	colorMode := fs.String("color", colorAuto, "color output: auto, always, never")
	all := fs.Bool("all", false, "show all images, including those without updates")
	configPath := fs.String("config", "", "YAML config file")
	progress := fs.Bool("progress", false, "show remote lookup progress on stderr")
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
	images, err := scanImages(path)
	if err != nil {
		return err
	}
	stdoutColors, err := newColors(*colorMode, os.Stdout)
	if err != nil {
		return err
	}
	stderrColors, err := newColors(*colorMode, os.Stderr)
	if err != nil {
		return err
	}
	updates := fetchUpdates(context.Background(), images, cfg, *progress, stderrColors)
	if !*all {
		updates = updatesWithAvailableUpdates(updates)
	}
	return outputUpdates(updates, *format, stdoutColors)
}

func loadConfig(path string) (Config, error) {
	cfg := Config{GitHubReleases: map[string]string{}}
	explicit := path != ""
	if !explicit {
		candidates, err := defaultConfigPaths()
		if err != nil {
			return cfg, err
		}
		for _, candidate := range candidates {
			if _, err := os.Stat(candidate); err == nil {
				path = candidate
				break
			} else if !os.IsNotExist(err) {
				return cfg, err
			}
		}
		if path == "" {
			return cfg, nil
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if !explicit && os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config %s: %w", path, err)
	}
	if cfg.GitHubReleases == nil {
		cfg.GitHubReleases = map[string]string{}
	}
	for image, repo := range cfg.GitHubReleases {
		if strings.TrimSpace(image) == "" || strings.TrimSpace(repo) == "" {
			return cfg, fmt.Errorf("config %s has an empty github_releases entry", path)
		}
	}
	return cfg, nil
}

func defaultConfigPaths() ([]string, error) {
	configDir, err := xdgConfigDir()
	if err != nil {
		return nil, err
	}
	return []string{
		cwdConfigPath,
		filepath.Join(configDir, "quadwatch", "config.yaml"),
	}, nil
}

func xdgConfigDir() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config"), nil
}

func pathArg(fs *flag.FlagSet) (string, error) {
	switch fs.NArg() {
	case 0:
		return ".", nil
	case 1:
		return fs.Arg(0), nil
	default:
		return "", fmt.Errorf("too many arguments: %s", strings.Join(fs.Args()[1:], " "))
	}
}

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
			return nil
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
	if t, ok := ref.(name.Tag); ok {
		tag = t.TagStr()
	}
	return Image{File: file, Image: raw, Repository: repo, Tag: tag}, true
}

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

func (f *updateFetcher) fetchOneGitHubRelease(ctx context.Context, img Image, githubRepo string) Update {
	u := Update{File: img.File, Image: img.Image, Repository: img.Repository, CurrentTag: img.Tag}
	tag, err := f.latestGitHubRelease(ctx, githubRepo)
	if err != nil {
		u.Error = err.Error()
		return u
	}
	newest, err := newestCompatible(img.Tag, []string{tag})
	if err != nil {
		u.Error = err.Error()
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

var versionRe = regexp.MustCompile(`^([^0-9]*?)([0-9]+(?:\.[0-9]+){0,3})(.*)$`)

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
	currentSegments := strings.Count(cm[2], ".") + 1
	var newestTag string
	var newestVersion *version.Version
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
		v, err := version.NewVersion(m[2])
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

type colors struct {
	enabled bool
}

func newColors(mode string, stream *os.File) (colors, error) {
	switch mode {
	case colorAuto:
		return colors{enabled: supportsColor(stream)}, nil
	case colorAlways:
		return colors{enabled: true}, nil
	case colorNever:
		return colors{}, nil
	default:
		return colors{}, fmt.Errorf("unknown color mode: %s", mode)
	}
}

func supportsColor(stream *os.File) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if colorEnvTruthy("CLICOLOR_FORCE") || colorEnvTruthy("FORCE_COLOR") {
		return true
	}
	if os.Getenv("CLICOLOR") == "0" || os.Getenv("TERM") == "dumb" {
		return false
	}
	if stream == nil {
		return false
	}
	info, err := stream.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func colorEnvTruthy(key string) bool {
	value, ok := os.LookupEnv(key)
	if !ok {
		return false
	}
	value = strings.ToLower(strings.TrimSpace(value))
	return value != "" && value != "0" && value != "false" && value != "no"
}

func (c colors) style(code, text string) string {
	if !c.enabled || text == "" {
		return text
	}
	return code + text + ansiReset
}

func (c colors) header(text string) string {
	return c.style(ansiBrightCyan, text)
}

func (c colors) dim(text string) string {
	return c.style(ansiDim, text)
}

func (c colors) red(text string) string {
	return c.style(ansiRed, text)
}

func (c colors) green(text string) string {
	return c.style(ansiGreen, text)
}

func (c colors) yellow(text string) string {
	return c.style(ansiYellow, text)
}

func (c colors) cyan(text string) string {
	return c.style(ansiCyan, text)
}

func (c colors) brightGreen(text string) string {
	return c.style(ansiBrightGreen, text)
}

func (c colors) brightYellow(text string) string {
	return c.style(ansiBrightYellow, text)
}

func (c colors) status(status string) string {
	switch status {
	case "ok":
		return c.green(status)
	case "update":
		return c.brightYellow(status)
	case "error":
		return c.red(status)
	default:
		return status
	}
}

func updateStatus(update Update) string {
	if update.Error != "" {
		return "error"
	}
	if update.Update {
		return "update"
	}
	return "ok"
}

type tableCellStyle func(row, col int, text string) string

func writeTable(out *os.File, headers []string, rows [][]string, colors colors, style tableCellStyle) error {
	widths := make([]int, len(headers))
	for col, header := range headers {
		widths[col] = displayLen(header)
	}
	for _, row := range rows {
		for col, cell := range row {
			if col >= len(widths) {
				continue
			}
			if width := displayLen(cell); width > widths[col] {
				widths[col] = width
			}
		}
	}

	if err := writeTableRow(out, headers, widths, colors, true, -1, style); err != nil {
		return err
	}
	for rowIndex, row := range rows {
		if err := writeTableRow(out, row, widths, colors, false, rowIndex, style); err != nil {
			return err
		}
	}
	return nil
}

func writeTableRow(out *os.File, cells []string, widths []int, colors colors, header bool, row int, style tableCellStyle) error {
	for col := range widths {
		if col > 0 {
			if _, err := fmt.Fprint(out, "  "); err != nil {
				return err
			}
		}
		text := ""
		if col < len(cells) {
			text = cells[col]
		}
		if col < len(widths)-1 {
			text = padRight(text, widths[col])
		}
		if header {
			text = colors.header(text)
		} else if style != nil {
			text = style(row, col, text)
		}
		if _, err := fmt.Fprint(out, text); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(out)
	return err
}

func displayLen(text string) int {
	return len([]rune(text))
}

func padRight(text string, width int) string {
	length := displayLen(text)
	if length >= width {
		return text
	}
	return text + strings.Repeat(" ", width-length)
}

func outputImages(images []Image, format string, colors colors) error {
	switch format {
	case "json":
		return json.NewEncoder(os.Stdout).Encode(images)
	case "csv":
		w := csv.NewWriter(os.Stdout)
		defer w.Flush()
		if err := w.Write([]string{"quadlet", "image_name", "current_tag"}); err != nil {
			return err
		}
		for _, i := range images {
			if err := w.Write([]string{i.File, i.Repository, i.Tag}); err != nil {
				return err
			}
		}
		return w.Error()
	case "table":
		rows := make([][]string, 0, len(images))
		for _, i := range images {
			rows = append(rows, []string{i.File, i.Repository, i.Tag})
		}
		return writeTable(os.Stdout, []string{"QUADLET", "IMAGE", "TAG"}, rows, colors, func(_ int, col int, text string) string {
			switch col {
			case 0:
				return colors.dim(text)
			case 1:
				return colors.cyan(text)
			case 2:
				return colors.yellow(text)
			default:
				return text
			}
		})
	default:
		return fmt.Errorf("unknown format: %s", format)
	}
}

func outputUpdates(updates []Update, format string, colors colors) error {
	switch format {
	case "json":
		return json.NewEncoder(os.Stdout).Encode(updates)
	case "csv":
		w := csv.NewWriter(os.Stdout)
		defer w.Flush()
		if err := w.Write([]string{"quadlet", "image_name", "current_tag", "newest_tag", "update", "error"}); err != nil {
			return err
		}
		for _, u := range updates {
			if err := w.Write([]string{u.File, u.Repository, u.CurrentTag, u.NewestTag, fmt.Sprint(u.Update), u.Error}); err != nil {
				return err
			}
		}
		return w.Error()
	case "table":
		if len(updates) == 0 {
			fmt.Fprintln(os.Stdout, colors.green("No images have an update."))
			return nil
		}
		rows := make([][]string, 0, len(updates))
		for _, u := range updates {
			rows = append(rows, []string{u.File, u.Repository, u.CurrentTag, u.NewestTag, u.Error})
		}
		return writeTable(os.Stdout, []string{"QUADLET", "IMAGE", "CURRENT", "NEWEST", "ERROR"}, rows, colors, func(row int, col int, text string) string {
			u := updates[row]
			switch col {
			case 0:
				return colors.dim(text)
			case 1:
				return colors.cyan(text)
			case 2:
				return colors.yellow(text)
			case 3:
				if u.Error != "" {
					return colors.red(text)
				}
				if u.Update {
					return colors.brightGreen(text)
				}
				return colors.dim(text)
			case 4:
				if u.Error != "" {
					return colors.red(text)
				}
				return colors.dim(text)
			default:
				return text
			}
		})
	default:
		return fmt.Errorf("unknown format: %s", format)
	}
}
