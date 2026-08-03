# quadwatch

<p align="center"><strong>Find container image updates in Quadlet files.</strong></p>

<p align="center">
  <a href="https://go.dev/"><img src="https://img.shields.io/badge/go-1.26.5%2B-00ADD8.svg" alt="Go 1.26.5+" /></a>
  <img src="https://img.shields.io/badge/quadlet-container%20images-blue.svg" alt="Quadlet container images" />
  <img src="https://img.shields.io/badge/output-json%20%7C%20csv%20%7C%20table-555.svg" alt="JSON, CSV, and table output" />
</p>

Small Go CLI for finding container images in Quadlet files and checking registry tags for newer compatible versions.

<code>quadwatch</code> recursively scans Quadlet files, extracts registry image references, lists remote tags, and reports the newest compatible tag using conservative prefix/suffix matching.

## Why use quadwatch

- Quadlet-aware scanning: reads `*.container`, `*.image`, and `*.volume` files.
- Registry lookup: checks public registries with a 30-second request timeout and uses Docker credential auth through the default Docker keychain.
- Automation-friendly output: supports `json`, `csv`, and human-readable `table` formats.
- Update-only by default: `fetch` reports only images with available updates unless `--all` is used.
- Colorized human output: table output and `--progress` statuses use terminal colors by default.
- Progress reporting: `--progress` shows lookup status on stderr while keeping machine-readable output on stdout.
- Conservative tag matching: compares version-like tags with the same prefix and suffix shape.
- Supports prefixed tags: handles tags such as `release-4.0.16.2944`, `v2.7.0`, and `1.2.3-alpine`.

## Quick Start

### 1. Install

```bash
curl -fsSL https://github.com/pgilad/quadwatch/releases/latest/download/install.sh | sh
```

Or build from source:

```bash
go build -o quadwatch .
```

### 2. List images

```bash
./quadwatch images --format table /path/to/quadlets
```

### 3. Fetch available updates

```bash
./quadwatch fetch --format csv /path/to/quadlets
```

### 4. Show all images and lookup progress

```bash
./quadwatch fetch --all --progress --format table /path/to/quadlets
```

## Commands

| Command | Purpose |
|---|---|
| `images PATH` | List detected images and current tags |
| `fetch PATH` | List images with newer compatible remote tags |
| `self-update` | Check the latest GitHub release and update the installed binary |
| `uninstall` | Remove the current `quadwatch` binary |
| `version` | Print the current version |
| `help` | Print usage |

Common options:

- `--format json|csv|table`
- `--color auto|always|never`: colorize human-readable table/progress output (default `auto`)
- `--all` for `fetch`: show all images, including those without updates
- `--config PATH` for `fetch`: load a YAML config file. If omitted, lookup uses `./quadwatch.yaml`, then `$XDG_CONFIG_HOME/quadwatch/config.yaml` (typically `~/.config/quadwatch/config.yaml`).
- `--progress` for `fetch`: show remote lookup progress on stderr

## Quadlet scanning

`images` and `fetch` recursively scan `PATH` for:

- `*.container`
- `*.image`
- `*.volume`

The scanner extracts image references from:

- `[Container] Image=`
- `[Image] Image=`
- `[Volume] Image=`

The parser intentionally mirrors the subset used by Renovate's Quadlet manager:

- Skips local/non-registry transports like `dir:`, `oci:`, `docker-archive:`, `oci-archive:`, `containers-storage:`, and `sif:`.
- Strips `docker://` and `docker-daemon:` prefixes.
- Applies Docker-style defaults: `docker.io` registry and `latest` tag when omitted.
- Detects digest-pinned references such as `repo:tag@sha256:...`; `fetch` skips them with `skip_reason=digest-pinned image` because the digest, not the tag, controls what runs.

## Tag compatibility

Version selection is heuristic. Tags are split into this shape:

```text
<prefix><version><suffix>
```

- `prefix`: any non-numeric prefix, including an empty prefix
- `version`: 1 to 4 dot-separated numeric segments, for example `1`, `1.2`, `1.2.3`, `1.2.3.4`
- `suffix`: anything after the version, including an empty suffix

Examples:

| Tag | Prefix | Version | Suffix |
|---|---|---|---|
| `v2.7.0` | `v` | `2.7.0` | empty |
| `release-4.0.16.2944` | `release-` | `4.0.16.2944` | empty |
| `nightly-1.2.3` | `nightly-` | `1.2.3` | empty |
| `foo_bar_10.5-alpine` | `foo_bar_` | `10.5` | `-alpine` |
| `5.2.3-1` | empty | `5.2.3` | numeric image revision `-1` |
| `2026.04.25.123345` | empty | `2026.04.25.123345` | empty |

Registry candidate tags must keep the same prefix and suffix as the current tag. A trailing numeric suffix is treated as an image-build revision, so numeric revisions are compatible with one another and participate in version ordering; for example, `5.1.4-2` can update to `5.2.3-1`, while neither tag matches `5.2.3-alpine`. GitHub Release lookups additionally treat a single leading `v` as optional and report the result using the current image tag's prefix style.

For current tag `release-4.0.16.2944`, accepted candidates include:

- `release-4.0.17.2952`
- `release-4.1.0.0000`

Ignored candidates include:

- `nightly-4.0.17.2952`
- `4.0.17.2952`
- `release-9831336`

For non-empty prefixes other than `v`, the candidate version must also have the same number of numeric segments as the current version. This avoids treating commit-like tags such as `release-9831336` as newer versions of tags like `release-4.0.16.2944`.

## Configuration

`fetch` can use a YAML config to source selected image updates from GitHub Releases instead of registry tag listing. This is useful for GHCR repositories with very large tag lists.

Default config lookup order:

1. `./quadwatch.yaml`
2. `$XDG_CONFIG_HOME/quadwatch/config.yaml` (typically `~/.config/quadwatch/config.yaml`)

Use `--config PATH` to load a specific file instead.

```yaml
github_releases:
  ghcr.io/immich-app/immich-server: immich-app/immich
  ghcr.io/immich-app/immich-machine-learning: immich-app/immich

repositories:
  ghcr.io/we-promise/sure:
    github_release: we-promise/sure
    include_prereleases: true
```

Config keys must use quadwatch's normalized repository name, not necessarily the image text as it appears in the Quadlet file. Quadwatch applies Docker-style defaults while scanning: Docker Hub images are normalized to `index.docker.io`, official Docker Hub images include the `library/` namespace, and tags are not part of the config key.

Examples:

| Quadlet `Image=` value | Config key |
|---|---|
| `postgres:16` | `index.docker.io/library/postgres` |
| `docker.io/library/postgres:16` | `index.docker.io/library/postgres` |
| `ghcr.io/immich-app/immich-server:v2.1.0` | `ghcr.io/immich-app/immich-server` |
| `quay.io/prometheus/prometheus:v3.0.0` | `quay.io/prometheus/prometheus` |

You can run `quadwatch images --format table PATH` to see the exact repository names to use as config keys.

`github_releases` is a shorthand for stable GitHub release lookups. For per-repository options, use `repositories`. Set `include_prereleases: true` to opt a specific normalized repository into prerelease-aware matching for tags such as `v0.7.2-alpha.10` and `v0.7.2-rc.1`. This option applies to both registry tag listing and GitHub release lookups; omit `github_release` to keep using registry tags.

Configured stable GitHub release entries run `gh release view -R <repo> --json tagName -q .tagName`. Repositories with `include_prereleases: true` and `github_release` configured run `gh release list -R <repo> --exclude-drafts --limit 100 --json tagName -q '.[].tagName'`, then compare those release tags using prerelease-aware compatibility rules. The `gh` CLI must be installed and authenticated if the GitHub request requires it.

GitHub Release lookups normalize an optional leading `v` to match the current image tag. For example, an image at `0.7.3-alpha.1` can use a GitHub release tagged `v0.7.3-alpha.4`; quadwatch reports the image-compatible tag `0.7.3-alpha.4`.

Prerelease matching is opt-in because Docker tag suffixes can also describe image flavors such as `-alpine`; only enable it for repositories where prerelease-style suffixes should be treated as part of the version.

## Output

### Images CSV

```csv
quadlet,image_name,current_tag
/path/app.container,index.docker.io/library/postgres,18.4-trixie
```

### Updates CSV

```csv
quadlet,image_name,current_tag,newest_tag,update,skip_reason,error
/path/sonarr.container,ghcr.io/hotio/sonarr,release-4.0.16.2944,release-4.0.17.2952,true,,
```

Table output uses `STATUS` (`ok`, `update`, `skipped`, or `error`) and `DETAILS` columns for skip reasons or errors. CSV output includes both `skip_reason` and `error` columns.

Non-version-like current tags, such as `latest`, are skipped before any registry/GitHub lookup and reported with `skip_reason=unsupported tag shape` when included with `fetch --all`; they are not treated as lookup errors.

## Install and update

Install the latest release:

```bash
curl -fsSL https://github.com/pgilad/quadwatch/releases/latest/download/install.sh | sh
```

Install a specific release:

```bash
curl -fsSL https://github.com/pgilad/quadwatch/releases/download/2026.05.23-5/install.sh | sh
```

Update or uninstall later:

```bash
quadwatch self-update
quadwatch uninstall
```

## Development

```bash
go test ./...
go build -o quadwatch .
```
