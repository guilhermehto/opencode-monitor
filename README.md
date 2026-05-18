# cogitator

`cogitator` is a terminal monitor for locally running [opencode](https://opencode.ai) instances.
It discovers instances over mDNS, subscribes to their event streams, and renders one live sessions view with attention signals (permission requests, pending questions, and errors).

## Install

### Go install

```sh
go install github.com/guilhermehto/cogitator/cmd/cogitator@latest
```

### Homebrew

Homebrew support is published through `guilhermehto/homebrew-tap` once release automation is configured.

## Supported OS

| OS | Support |
| --- | --- |
| macOS | Supported |
| Linux | Supported |
| Windows | Not supported |

## Prerequisite

Each opencode instance you want to monitor must be launched with `--mdns` so
it advertises itself on `_http._tcp.local.`:

```sh
opencode --mdns                       # default port (random)
opencode serve --mdns --port 7777     # headless, fixed port
```

You can launch as many as you like; cogitator discovers them automatically.

## Run

```sh
cogitator
```

or from source:

```sh
go run ./cmd/cogitator
```

`q`, `Esc`, or `Ctrl+C` quits.

## CLI reference

- `--bell`: ring the terminal bell when a session transitions into an attention state.
- `--status`: print a one-shot icons-only status line and exit.
- `--log-level`: set log verbosity (`debug|info|warn|error`). Default is `info`.
- `--version`: print module version, commit, and build date.

## Logging

Logs are written with `log/slog` text formatting.

- If `$XDG_STATE_HOME` is set: `$XDG_STATE_HOME/cogitator/cogitator.log`
- Otherwise: `/tmp/cogitator.log`

## Architecture overview

- `internal/discovery`: mDNS browsing and add/remove events for opencode instances.
- `internal/supervisor`: per-instance lifecycle (permissions poll, recency poll, SSE loop, reconnect backoff).
- `internal/oc`: HTTP + SSE API access and generated OpenAPI-derived core types.
- `internal/state`: in-memory aggregation and dedupe across instances, attention classification, unreachable-instance tracking.
- `internal/ui`: Bubble Tea model, rendering, status mode, and footer warnings.
- `internal/config`: single source of timing/threshold defaults.

## Status mode

`--status` runs discovery/supervision without opening the TUI and prints a compact status line.
It exits when either:

- a non-empty snapshot arrives, or
- the status deadline is reached (default: 3s).

## Notes for macOS unsigned binaries

Current releases are unsigned. If Gatekeeper blocks first launch, either use Finder "Open" once, or clear quarantine:

```sh
xattr -d com.apple.quarantine cogitator
```

## Development

Common local targets:

```sh
make vet
make lint
make test
make ci
```

OpenAPI workflow:

```sh
make capture-schema
make generate
```

## Roadmap

- macOS code signing + notarization (blocked on Apple Developer Program enrolment).
- OpenAPI-derived SSE event payload types (blocked on opencode publishing the event-stream schema).
