# tilt-tui

A terminal UI for a running [Tilt](https://tilt.dev) instance. It talks to Tilt's local API, so you can watch resources and read their logs without leaving the terminal.

This is a Go rewrite of [andycmaj/tilt-tui](https://github.com/andycmaj/tilt-tui), which is written for Bun. Thanks to Andy for the original design and the config format.

Binaries ship for linux and darwin on amd64 and arm64. There's no Windows build.

## Install with the script

This is the recommended path. Each step runs only if the one before it succeeded, so a failed download or a failed attestation check never runs the script.

```bash
curl -fsSLO https://github.com/SentioLabs/go-tilt-tui/releases/latest/download/install.sh &&
  gh attestation verify install.sh --repo SentioLabs/go-tilt-tui \
    --signer-workflow SentioLabs/go-tilt-tui/.github/workflows/release.yml &&
  bash install.sh
```

The one-liner below runs a script you haven't verified. The script still verifies everything it installs.

```bash
curl -fsSL https://github.com/SentioLabs/go-tilt-tui/releases/latest/download/install.sh | bash
```

The script checks the archive's sha256 against `checksums.txt`. It then runs `gh attestation verify` with the repo and the signer workflow. That second check needs `gh` logged in (`gh auth login`, or set `GH_TOKEN`). Without a login, `gh` exits before it checks anything and the install stops.

The script never uses `sudo`. These environment variables change what it does:

| Variable | Meaning | Default |
| --- | --- | --- |
| `TILT_TUI_VERSION` | Tag to install, for example `v0.2.0` | latest release |
| `TILT_TUI_INSTALL_DIR` | Directory for the `tilt-tui` binary | `~/.local/bin` |
| `TILT_TUI_INSECURE_SKIP_ATTESTATION` | Set to `1` to skip the `gh` check. The sha256 check still runs. | unset |
| `TILT_TUI_BASE_URL` | Release host | `https://github.com/SentioLabs/go-tilt-tui` |

An install right after a release can fail for a minute or two while the assets upload. Retry.

## Install with mise

```bash
mise use -g github:SentioLabs/go-tilt-tui@0.1.0
```

Or in a `mise.toml`:

```toml
[tools]
"github:SentioLabs/go-tilt-tui" = "0.1.0"
```

The mise docs say that on public GitHub, mise checks GitHub artifact attestations when a release asset has them. It also checks the digest GitHub reports or a published checksum file, and records the checksum in `mise.lock`. The docs don't say what happens when a release has no attestation. This was checked against the mise 2026.10.19 docs. For the strictest check, use the script path above or [verify by hand](#verify-a-download-by-hand).

## Update

Update to the newest release:

```bash
tilt-tui self update
```

It asks before it replaces the binary. Pass `-y` to skip the prompt, or `--force` to reinstall the current version. `tilt-tui self update --check` prints whether an update exists and installs nothing.

Every update checks the archive's sha256 against the release's `checksums.txt`. Before it trusts that file, it runs `gh attestation verify` on it with the repo and the signer workflow, and a failed check stops the update. The install script runs the same check on the archive itself. Both use the release workflow's attestation. The `gh` step needs `gh` logged in (`gh auth login`, or set `GH_TOKEN`). Set `TILT_TUI_INSECURE_SKIP_ATTESTATION=1` to skip it. The sha256 check still runs.

`tilt-tui self channel` prints the update channel. `tilt-tui self channel rc` switches to `rc` after a prompt. The channels are `stable`, `rc` and `nightly`. Only stable releases are published today. The channel lives in `selfupdate.toml` in the config directory, next to `config.toml`.

`self update` refuses a binary that mise, Homebrew, a system package or Nix installed. It prints the manager's upgrade command instead. For mise that's `mise upgrade github:SentioLabs/go-tilt-tui`.

## Verify a download by hand

Download an archive, `checksums.txt` or `install.sh` from the release page, then run:

```bash
gh attestation verify <file> --repo SentioLabs/go-tilt-tui \
  --signer-workflow SentioLabs/go-tilt-tui/.github/workflows/release.yml
```

Check an archive against the checksums with one of these:

```bash
sha256sum -c checksums.txt --ignore-missing
shasum -a 256 -c checksums.txt --ignore-missing
```

For an offline check, download the attestation bundle and pass it with `--bundle`.

## Moving from the Bun tilt-tui

An older `tilt-tui` earlier on your `PATH` shadows this one. Remove it first. With mise:

```bash
mise use -g --remove github:andycmaj/tilt-tui
command -v tilt-tui
```

The second command should print the path of the new binary.

## Usage

Attach to a Tilt that's already running:

```bash
tilt-tui
```

Start Tilt and stop it when you quit:

```bash
tilt-tui --up -- -f Tiltfile
```

| Flag | Meaning |
| --- | --- |
| `--host` | Tilt API host |
| `--port` | Tilt API port. Defaults to `$TILT_PORT`, else 10350. |
| `--allow-remote` | Allow a non-loopback `--host`. Tilt's tokens then travel unencrypted. Not valid with `--up`. |
| `--up` | Run `tilt up --port PORT ARGS...` with the arguments after `--`, and stop it on quit |
| `--version` | Print the version and exit |

Config lives in `$XDG_CONFIG_HOME/tilt-tui/config.toml`, else `~/.config/tilt-tui/config.toml`. The keys `theme`, `tiltBinaryPath`, `logFilters` and `logMaxBytesPerResource` match the Bun version. `mouseWheelList` is new. Filter patterns use RE2, which has no lookaround or backreferences.

Click a resource to select it, and click a header twice to collapse or expand its group. The wheel over the list moves the selection one row per notch, and over the logs it scrolls them. A click in a pane gives it focus. While the app reads the mouse, most terminals select text only with Shift held (Option in iTerm2). Set `mouseWheelList = "scroll"` to make the wheel scroll the list and keep the selection. Press `,` for the settings pane, which changes it for this session.

A local `go build` prints a version like `v0.0.0-<date>-<commit>`, with `+dirty` on a modified tree. Release builds print the tag.

## Development

Install the tools with `mise install` (mise `2026.10.19` or newer). Then run every gate:

```bash
make check
```

| Target | What it does |
| --- | --- |
| `build` | Build `bin/tilt-tui`. `make build VERSION=v0.1.0` sets the version. |
| `test` | `go test -race ./...` |
| `fmt-check`, `tidy-check`, `vet` | gofmt, `go mod tidy -diff` and `go vet` |
| `lint` | golangci-lint |
| `vuln` | osv-scanner over the module graph and stdlib |
| `pin-check` | Fail on a workflow `uses:` that isn't a commit SHA |
| `shellcheck`, `actionlint` | Lint the scripts and workflows |
| `install-test` | Test `scripts/install.sh` |
| `release-snapshot` | `goreleaser release --snapshot --clean` |

Releases follow conventional commits on `main`. release-please opens a release PR, and merging it publishes. See [docs/release.md](docs/release.md).

## License

MIT. See [LICENSE](LICENSE).
