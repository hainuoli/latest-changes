# Implementation status

The Go server is implemented in `cmd/latest-changes` and `internal/`.
The module and executable are named `latest-changes`.

Implemented:

- HMAC-SHA256 verification and bounded webhook request bodies.
- RSA App JWTs and installation tokens scoped to exactly one repository.
- Current PR state reads, complete file pagination, and label status reporting.
- Ordered recursive path rules, exclusions, rename handling, and manual labels.
- Deterministic release notes with upstream golden fixtures and SHA-conflict retries.
- Configurable, validated single-line Gitmoji commit titles with no body.
- Persistent single-process webhook queue, deduplication, retries, and restart recovery.
- Server timeouts, graceful shutdown, configuration, CI, and operation documentation.

Local validation completed using Go 1.25.4:

- `go mod verify`
- `go vet ./...`
- `go build -trimpath -o /private/tmp/latest-changes-server ./cmd/latest-changes`
- `go test -race -count=1 -coverprofile=... ./...`
- YAML parsing of both root GitHub configuration files.
- 28 top-level Go tests, including table-driven subtests and 14 rendering golden cases.
- Complete webhook-to-mock-GitHub integration, with no live GitHub writes.

The pinned upstream snapshot remains unchanged. Its pytest suite was not run.
Python-specific pyproject dependency semantic classification and its release CLI
are intentionally not ported. Full GitHub App installation and a live PR test
remain deployment steps; they have not been performed or claimed.

No commits, pushes, remote repositories, GitHub forks, or deployments were created.
Agent-created commits require individual approval and a title-only Gitmoji message.

Code organization was reviewed against go-zero's handwritten core libraries.
The server uses named constructors and private implementation fields, while API
operations have explicit verbs. Event orchestration, classification, note updates,
GitHub transport and authentication are separated by responsibility. Structs,
configuration literals and tests use multiline layout. These conventions are
recorded in `AGENTS.md`; go-zero is not a runtime dependency.
