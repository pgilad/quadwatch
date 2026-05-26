# quadwatch

<p align="center"><strong>Find container image updates in Quadlet files.</strong></p>

<p align="center">
  <a href="https://go.dev/"><img src="https://img.shields.io/badge/go-1.26.3%2B-00ADD8.svg" alt="Go 1.26.3+" /></a>
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
curl -fsSL https://raw.githubusercontent.com/pgilad/quadwatch/main/scripts/install.sh | sh
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
| `2026.04.25.123345` | empty | `2026.04.25.123345` | empty |

Candidate tags must keep the same prefix and suffix as the current tag.

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

Configured entries run `gh release view -R <repo> --json tagName -q .tagName`, then compare that release tag using the same compatibility rules as registry tags. The `gh` CLI must be installed and authenticated if the GitHub request requires it.

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
curl -fsSL https://raw.githubusercontent.com/pgilad/quadwatch/main/scripts/install.sh | sh
```

Install a specific release:

```bash
curl -fsSL https://raw.githubusercontent.com/pgilad/quadwatch/main/scripts/install.sh | sh -s -- --version 2026.05.23-5
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
