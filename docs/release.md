# Release pipeline

This page explains how a release happens and what to change when you copy the pipeline to another repo, such as envctl or arc.

## How a release happens

1. A push to `main` runs `.github/workflows/release.yml`.
2. Its `ci` job calls `ci.yml`.
3. The `release-please` job runs only after CI passes. It opens or updates a PR titled `chore(main): release X.Y.Z`.
4. Merging that PR makes release-please create the tag and the GitHub release.
5. The `publish` job runs `goreleaser release --clean`. That builds four archives and `checksums.txt`, and uploads `install.sh` as an extra file.
6. `actions/attest-build-provenance` then attests `dist/*.tar.gz`, `dist/checksums.txt` and `scripts/install.sh`.

Only `publish` has `id-token` and `attestations` write. The `release-please` job has `contents`, `issues` and `pull-requests` write. It needs `issues` to label its PRs.

The `publish` job sets `cache: false` on mise-action. The release tools are downloaded fresh and checked against `mise.lock`.

## Versioning

The release-please manifest starts at `0.0.0`. `initial-version: 0.1.0` makes the first release `0.1.0`. `bump-minor-pre-major` turns breaking changes into minor bumps before 1.0. A one-time `release-as: 1.0.0` cuts 1.0 later.

A plain local build prints a Go VCS version such as `v0.0.0-20261104014620-9ac13a00352d`, with `+dirty` on a modified tree. Without ldflags, the build date falls back to the commit time. `make build VERSION=v0.1.0` sets the version. Release builds print the tag.

## Known limits

- **Assets lag the release.** For a minute or two after the release PR merges, `releases/latest` has no assets or attestations. Installs fail closed in that window.
- **The release PR shows no checks.** release-please opens its PR with `GITHUB_TOKEN`, so GitHub starts no workflows for it. That's safe because the merge push runs `ci.yml` before anything publishes.
- **Fast pushes can skip a run.** The `release` concurrency group sets `cancel-in-progress: false`. GitHub still cancels an older pending run when a newer one queues. Three quick pushes to `main` can skip the middle run's CI.
- **Lint reports are capped.** golangci-lint's default caps (3 identical findings, 50 per linter) can hide findings in a report. Zero findings is still zero.

## Pinning and updates

Every `uses:` is a commit SHA with a `# vX.Y.Z` comment. `make pin-check` enforces it.

mise pins every tool in `mise.toml`, and `mise.lock` holds the sha256 for each platform. The lock is enforced with `[tool_config] locked = true`. That block is project-scoped. A `[settings] locked` entry would also lock the tools in your global config. CI's mise-action adds `--locked` itself.

Dependabot bumps actions and Go modules weekly with a 7-day cooldown. It doesn't read `mise.toml`, so bump tools by hand:

1. Edit the version in `mise.toml`.
2. Run `mise lock --bump <tool>`. Plain `mise lock` fails after a version edit.
3. Commit as `chore(mise): ...`.

To add a brand-new tool, remove the `[tool_config]` block, run `mise lock`, then restore the block.

A Go bump changes three values in one commit: the `go` line in `go.mod`, `go` in `mise.toml` and `GoVersionOverride` in `osv-scanner.toml`. osv-scanner 2.7.0 matches stdlib advisories against the `go` line in `go.mod`. `make vuln` fails if the three differ.

## Vulnerability scan

`make vuln` runs `scripts/vuln_scan.sh`. It runs osv-scanner 2.7.0 over the `go.mod` module graph and the standard library, with call analysis. It repeats the scan once per release target, with `GOOS` and `GOARCH` set to match goreleaser. `ScanGoModVersion = true` in `osv-scanner.toml` keeps stdlib in the scan.

The scan fails on either degraded-analysis message: "Failed to run code analysis" or "Skipping call analysis". Call analysis only runs when some advisory matches.

Every `[[IgnoredVulns]]` entry in `osv-scanner.toml` needs a `reason` and an `ignoreUntil` date.

## Porting to envctl or arc

Change these values:

| Where | What to change |
| --- | --- |
| `.goreleaser.yaml` | `project_name`, `main`, `binary` |
| `.goreleaser.yaml` ldflags | The package path of `Version`, and the `v` prefix on it |
| `.goreleaser.yaml` | Archive `name_template` |
| `.goreleaser.yaml` | `release.github.owner` and `release.github.name` |
| `.goreleaser.yaml` | `extra_files` (the install script path) |
| `.goreleaser.yaml` | The checksum file name must stay `checksums.txt`. The attest glob in `release.yml` expects it. |
| `scripts/install.sh` | `REPO`, `SIGNER`, the binary name and the env var prefix |
| `README.md` | The signer workflow path in every `gh attestation verify` command |
| `mise.toml` | The tool list |
| `mise.toml` | `[tool_config] locked = true` |
| `.golangci.yaml` | The depguard rule (it is project-specific) |
| `release-please-config.json` | `initial-version`. Drop it for a repo that already has releases. |
| `release.yml` | `issues: write` on the `release-please` job |
| `release.yml` | `cache: false` on the `publish` job's mise-action |
| `go.mod`, `mise.toml`, `osv-scanner.toml` | The three-way Go version match |

Also do these steps:

1. Delete goreleaser's `changelog:` block. It has no effect under release-please, since goreleaser's default `release.mode` is `keep-existing`.
2. Drop the `before.hooks: go mod tidy` entry.
3. Replace tag pins with commit SHAs.
4. Add `ci.yml`.
5. Turn on per-target call analysis. bacstack's scan doesn't run it.
