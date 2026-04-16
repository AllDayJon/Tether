# Contributing to Tether

## Prerequisites

- Go 1.22+
- A shell with bash, zsh, or fish

## Building

```sh
git clone https://github.com/AllDayJon/Tether
cd Tether
go build .        # builds ./tether binary
go test ./...     # run tests
go vet ./...      # static analysis
```

## Branch workflow

- `main` — stable, matches the latest release
- `dev` — integration branch; open PRs against this

PRs should target `dev`. When a release is ready, `dev` is merged into `main` and a version tag triggers the release workflow.

## Making changes

1. Fork the repo and create a branch off `dev`
2. Make your changes
3. Run `go test ./...` and `go vet ./...` — both must pass
4. Open a PR against `dev` with a clear description of what changed and why

## Commit style

Use a short imperative subject line, optionally followed by a blank line and a longer description:

```
fix: handle empty SHELL env var on macOS

Falls back to /bin/sh when $SHELL is unset, which can happen
in non-interactive environments like launchd services.
```

Common prefixes: `feat`, `fix`, `refactor`, `docs`, `test`, `chore`.

## Reporting bugs

Open an issue at https://github.com/AllDayJon/Tether/issues with:
- What you did
- What you expected
- What actually happened
- Your OS, shell, and `tether version` output
