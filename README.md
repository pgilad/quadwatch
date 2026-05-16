# quadlet-updates

Small Go CLI for finding container images in Quadlet files and checking registry tags for newer compatible versions.

## Commands

```bash
quadlet-updates images [--format json|csv|table] PATH
quadlet-updates fetch  [--format json|csv|table] PATH
```

`images` recursively scans `PATH` for `*.container`, `*.image`, and `*.volume` files and extracts:

- `[Container] Image=`
- `[Image] Image=`
- `[Volume] Image=`

`fetch` does the same scan, then lists remote registry tags and reports the newest compatible tag.

## Examples

```bash
go run . images --format table ../homelab-ansible/roles/cooper_quadlets/files/quadlets

go run . fetch --format csv ../homelab-ansible/roles/cooper_quadlets/files/quadlets
```

## Compatibility notes

The parser intentionally mirrors the subset used by Renovate's Quadlet manager:

- skips local/non-registry transports like `dir:`, `oci:`, `docker-archive:`, etc.
- strips `docker://` and `docker-daemon:` prefixes
- supports public registry lookups and Docker credential auth via the default Docker keychain

Version selection is heuristic: it compares version-like tags with the same prefix/suffix shape, e.g. `v1.2.3` to `v1.2.4`, or `1.2.3-alpine` to `1.2.4-alpine`.
