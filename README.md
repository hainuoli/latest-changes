# Latest Changes

A GitHub Action written in Go. It classifies pull requests, reports
`latest-changes/label`, and updates release notes after merges. Release-note
commits contain a single Gitmoji title with no body, trailers, or `[skip ci]`.
No server, webhook, App registration, private key, or personal access token is
required. The default identity is `github-actions[bot]`.

The behavior is adapted from
[tiangolo/latest-changes-app](https://github.com/tiangolo/latest-changes-app).
The original MIT notice is retained in [LICENSE](LICENSE), and the upstream
revision is documented in [UPSTREAM.md](UPSTREAM.md). Implementation and tests
are written in Go; the upstream source is not bundled.

## Use

Publish this repository, then copy [examples/latest-changes.yml](examples/latest-changes.yml)
to `.github/workflows/latest-changes.yml` in the target repository. Replace
`hainuoli/latest-changes@main` with a reviewed full commit SHA before production use.
The action builds its own source with Go on the runner. A checkout of the target
repository is unnecessary.

The workflow listens to `pull_request_target` for opened, edited, synchronize,
reopened, labeled, unlabeled, and closed events. This supports contributions
from forks while running trusted action code. It reads PR metadata and changed
paths through the API; it never checks out or executes PR code. Keep it separate
from workflows that build or test contributions, and never add a PR-head checkout
to this privileged job.

Grant only these repository permissions in the calling workflow:

```yaml
permissions:
  contents: write
  pull-requests: write
  statuses: write
```

Pass `${{ secrets.GITHUB_TOKEN }}` to the action's `token` input. GitHub provides
and expires this token for each job; no repository secret needs to be created.
Standard GitHub-hosted runners are free for public repositories. Private
repository usage is subject to the account's Actions allowance and billing.

The example serializes runs for the repository with `queue: max`, preserving up
to 100 pending runs rather than replacing pending merge events. Runs already
in progress are not canceled. GitHub's queue limits still apply. If a run fails
or is canceled, rerun it in Actions, or use **Run workflow** and enter its PR
number. Recovery re-reads the current PR: open PRs are classified again, merged
PRs update notes, and closed unmerged PRs are skipped.

`latest-changes/label` is the required classification status; the workflow job's
completion alone does not mean a category has been selected. Require this status
from GitHub Actions only after a successful demo. `GITHUB_TOKEN` cannot bypass
branch rules that prohibit direct file updates. If your default branch requires
all changes to go through PRs, this direct-write version needs a separately
approved authentication or PR-based update design; do not weaken protection as
part of setup.

Disable the old Latest Changes App for the target repository before enabling
this workflow, so only one implementation manages its labels and release notes.
Remove its old ruleset bypass and status source when completing the migration.
Keep the shared path-rule configuration below.

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

The official hosted App does not support `commit-title`; disable the old App
before adding it. A configuration file must contain an `auto-labels` mapping;
use `auto-labels: {}` for manual classification. With no configuration file,
the action checks manual labels and uses the default commit title.

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
configuration, the action selects and normalizes one category.

Dependency updates are classified through path rules rather than dependency-file
semantics. This action handles pull-request labels and release-note updates;
release preparation is outside its scope.

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

## Execution and recovery

The process reads `GITHUB_EVENT_PATH`, verifies that its repository name and ID
match `GITHUB_REPOSITORY` and the API, refreshes the PR, and handles one event.
Configuration is read from the current default branch. PR titles, branch names,
and file contents are treated as data, not interpolated into shell scripts.
The run has a 90-second processing deadline; each API request has a five-second
timeout. Operational errors produce a nonzero exit code and appear in Actions. There is
no local queue or persistent service to maintain.

The default `GITHUB_TOKEN` does not trigger new push workflows for release-note
commits, so `[skip ci]` is unnecessary. Label changes made with this token also
do not start additional labeled/unlabeled workflow runs; the action computes
the final classification status in the same run. An optional custom `token`
may change these triggering behaviors and must have the same permissions.

Existing release notes and SHA-conflict retries make reruns idempotent. A
canceled, failed, or queue-rejected run must be rerun manually; this action does
not poll for missed events. The provided `workflow_dispatch` input is the
recovery mechanism.

## Development

Requires Go 1.23 or newer. The composite action sets up the stable Go release.

```sh
go mod verify
go vet ./...
go test -race -count=1 ./...
go build -trimpath -o bin/latest-changes ./cmd/latest-changes
```

For local execution, set `GITHUB_TOKEN`, `GITHUB_REPOSITORY`,
`GITHUB_EVENT_NAME`, and `GITHUB_EVENT_PATH` to a saved event JSON file.
Supported events are `pull_request`, `pull_request_target`, and
`workflow_dispatch`; the last also requires `LATEST_CHANGES_PULL_REQUEST`.
Local execution with a real token can write to GitHub. Tests use only local
mock servers and temporary event files and do not require live credentials.

Tests cover the Actions-to-mock-GitHub flow, recovery, repository identity
checks, label selection, current-head statuses, file pagination, conflicts,
reruns, and title-only commits. Rendering is checked against 14 upstream golden
outputs. Production dependencies are limited to YAML parsing and recursive
path matching. HTTP and JSON use the Go standard library.

Agent-created commits require individual approval and a single-line Gitmoji
message, as described in [AGENTS.md](AGENTS.md).

References: [token-triggered events](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/trigger-a-workflow),
[workflow concurrency](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/control-workflow-concurrency),
and [Actions billing](https://docs.github.com/en/billing/concepts/product-billing/github-actions).
