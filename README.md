# Latest Changes

A self-hosted GitHub App written in Go. It classifies pull requests, reports
`latest-changes/label`, and updates release notes after merges. Automated
release-note commits contain a single Gitmoji title with no body.

This implementation adapts the behavior of
[tiangolo/latest-changes-app](https://github.com/tiangolo/latest-changes-app).
The original MIT notice and pinned source snapshot are retained; see
[UPSTREAM.md](UPSTREAM.md). The application runs entirely in Go.

## Run

Requires Go 1.23 or newer and a GitHub App installation. Build the server:

```sh
go build -trimpath -o bin/latest-changes ./cmd/latest-changes
```

Configure the environment before starting it:

```sh
export GITHUB_CLIENT_ID='your-app-client-id'
export GITHUB_APP_PRIVATE_KEY_FILE='/absolute/path/to/app.pem'
export GITHUB_WEBHOOK_SECRET='your-random-webhook-secret'
export DATA_DIR='/absolute/path/to/persistent-data'
export LISTEN_ADDR='127.0.0.1:8080'
./bin/latest-changes
```

`GITHUB_APP_PRIVATE_KEY` can supply the PEM directly instead of a file. Set only
one private-key source. Environment files are examples, not automatically loaded.
`LISTEN_ADDR` defaults to `:8080`; `DATA_DIR` defaults to `./data`.
`GET /health` checks the running HTTP service.

Put the HTTP service behind HTTPS. Register `/webhooks/github` as the App's
webhook URL, configure the same webhook secret, and subscribe to Pull request
events. Grant these repository permissions:

| Permission | Access |
| --- | --- |
| Contents | Read and write |
| Pull requests | Read and write |
| Commit statuses | Read and write |
| Metadata | Read |

Install the App on the intended repositories. Installation tokens are checked
to cover exactly one target repository and the requested permissions. If a
ruleset prevents direct release-note writes, allow this App to bypass it.
Require `latest-changes/label` from this App only after verifying a test PR.
Remove the old App when switching; two Apps should not manage the same notes.

## Repository configuration

Add `.github/latest-changes.yml` to each managed repository:

```yaml
commit-title: "📝 Update release notes"
auto-labels:
  docs:
    - docs/**
    - README*.md
    - CONTRIBUTING.md
    - SECURITY.md
  upgrade:
    - go.mod
    - go.sum
  internal:
    - .github/**
    - .gitignore
    - .gitattributes
    - AGENTS.md
```

`commit-title` is an extension of this Go implementation. Its default is
`📝 Update release notes`. It requires one supported Gitmoji, a space, a
capitalized English subject, and at most 72 Unicode code points. Consecutive
spaces, trailing whitespace, a trailing period, and control characters are
rejected. No body, trailers, or `[skip ci]` is added. The supported emoji are
✨ 🐛 💥 🔒 📝 ✏ 🌐 ⬆ ⬇ ♻ 🎨 ⚡ 🔥 👷 🔧 🔨 🚨 ✅ 🔖 🎉 📄 🙈,
with an optional trailing variation selector.

The official hosted App does not support `commit-title`; switch App identities
before adding it. A configuration file must contain an `auto-labels` mapping;
use `auto-labels: {}` for manual classification. With no configuration file,
the App checks manual labels and uses the default commit title.

Path rules match repository-relative POSIX paths, including recursive `**`.
Rules are ordered; the first matching rule for a label wins. For example:

```yaml
auto-labels:
  docs:
    - exclude: docs/generated/**
    - docs/**
```

Every changed path must be classified for automatic selection, including both
paths of a rename. Mixed source and documentation changes wait for a manual
category. Automatic selection runs on `opened` and `synchronize`; label changes
respect manual selection. New commits may promote a category by priority.
Other labels such as `help wanted` are preserved.

Supported categories in precedence and output order:

| Label | Section |
| --- | --- |
| breaking | Breaking Changes |
| security | Security Fixes |
| feature | Features |
| bug | Fixes |
| refactor | Refactors |
| upgrade | Upgrades |
| docs | Docs |
| lang-all | Translations |
| infra | Infrastructure |
| internal | Internal |

The `release` label skips the release-note update and is mutually exclusive
with category labels. Without automatic-label configuration, zero category
labels produce a pending status and multiple labels produce failure. With
configuration, the App selects and normalizes one category.

Unlike the upstream Python application, this version does **not** semantically
classify dependency changes inside `pyproject.toml`. It can still classify that
file through an explicit path rule. The upstream release-preparation Python
CLI and OpenAPI documentation UI are not part of this server.

## Release notes

The first existing file is used, in this order:

1. `release-notes.md`
2. `docs/release-notes.md`
3. `docs/en/docs/release-notes.md`
4. `CHANGELOG.md`

If no file exists, `release-notes.md` is created. Existing files must contain
`## Latest Changes`. Its level-three headings must use the supported section
names without duplicates. Historical releases are preserved. The renderer is
tested against golden outputs generated from the pinned upstream implementation.
Repeated delivery does not duplicate an existing entry; conflicting file writes
re-read the document and retry up to three times.

Since commits do not include `[skip ci]`, they can trigger push workflows. If
needed, filter only the relevant workflows' `push` event for changes exclusively
to the notes file. Keep required PR checks running; PR path filters can leave
required checks pending.

## Delivery and operations

Signed, valid pull-request events are written to disk before HTTP `202` is
returned. A single worker processes events, with a 90-second processing deadline
and five attempts using exponential backoff. GitHub API requests have a separate
five-second timeout. Pending work survives server restarts. Repeated delivery IDs
are deduplicated, and completed records are retained for seven days. Content
idempotency still protects release notes after that retention period.

The default queue holds up to 1,000 pending jobs. A persistence or capacity error
returns `503`, not success. Exhausted tasks remain on disk with `failed: true`
and are logged. Inspect and fix the cause, then manually redeliver from GitHub's
App delivery page; the same failed delivery ID can be retried. There is no
automatic polling of GitHub for deliveries missed while the server was down.

Run **one process with one persistent local data directory**. Do not share the
spool between replicas or place it on an ephemeral filesystem. File and directory
syncs are used before acknowledging writes; storage durability still depends on
the host filesystem. Failed records require operator cleanup after resolution.
The health endpoint reports process health, not an empty failure backlog.

## Development

```sh
go mod verify
go vet ./...
go test -race -count=1 ./...
go build ./...
```

Tests use local mock GitHub servers and temporary queues; no live GitHub writes
or App credentials are needed. Production dependencies are limited to YAML
parsing and recursive path matching. Authentication and HTTP use the Go standard
library.

Before every agent-created commit, review the changes, test results, and exact
proposed single-line Gitmoji title. Explicit approval is required for each
commit, as described in [AGENTS.md](AGENTS.md).
