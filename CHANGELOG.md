# Changelog

## 2026.08.27-132

- feat: add Quadlet update and pin commands (e8e5da9)

## 2026.08.26-131

- feat: support digest-pinned image updates (bd6a7aa)

## 2026.08.03-82

- ci: attest release artifacts (a98b075)

## 2026.08.03-81

- fix: harden release provenance and updates (6199b9b)

## 2026.08.03-80

- fix: support numeric image revisions (f18a522)

## 2026.07.24-69

- fix: normalize GitHub release v prefixes (d31b068)

## 2026.06.27-42

- chore: update go dependencies (1be708c)
- Support pre-release (bdd6752)

## 2026.05.26-9

- fix: improve fetch reliability and output consistency (ca979bc)
- fix: cache registry lookups and support cancellation (42c2224)
- fix: skip digest-pinned images and surface fetch errors (2825ea1)

## 2026.05.23-6

- Add License (a0b2986)

## 2026.05.23-5

- Initial quadlet updates CLI (3ce4fb1)
- Add configurable update fetching (fed23bf)
- Use XDG config fallback (dede32d)
- Fix compatible tag version selection (4cd46db)
- Rename project to quadwatch (d60057c)
- Add mise build and install tasks (7842c42)
- Add CI and release workflow (698fe2a)
- Improve test coverage and refactor CLI (b7874cc)
- Add installer and self-update commands (8a0efa1)
- Add scheduled release workflow (480f877)
- Use golangci-lint action v7 (41dd6d7)
- Use Go 1.25 for lint compatibility (db283a1)
- Use golangci-lint action v9 (69607f1)
- Use Go 1.26.3 (f2b1545)
- Fix lint errors (ec90ab3)
- Enable cgo for race tests (d13fef6)
- Remove pull request CI trigger (2abf9fd)
- chore: update GitHub Actions versions (1b565b1)
- chore: update golangci-lint version (969979b)
