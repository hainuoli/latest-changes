# Repository instructions

- Ask the user for explicit approval before every commit, showing the changes,
  validation results, and exact proposed title. Approval covers only one commit.
- Agent-created commits must contain only a single Gitmoji title, at most 72
  characters, with an English imperative sentence starting with a capital
  letter and no trailing period. Do not add a body or trailers.
- Do not push, create a remote repository or pull request, or deploy without
  the user's approval.
- Keep implementation and tests in Go. Do not vendor Python source, tooling,
  or lockfiles. Preserve the upstream attribution in `UPSTREAM.md` and the
  original MIT copyright and permission notice in `LICENSE`.

## Go code style

- Follow the handwritten go-zero core style for naming and layout; keep the
  current lightweight package structure and avoid generated service scaffolding.
- Use `NewType` constructors and private implementation fields. Define small
  dependency interfaces in the package that consumes them. Give API methods
  explicit actions, such as `GetPullRequest` and `UpdateFile`.
- Group related type declarations. Write structure fields and keyed multi-field
  literals on separate lines. Separate declarations and logical steps with blank
  lines, and keep related operations in clearly named lowercase files.
- Prefer early returns and focused helpers to deeply nested event handlers.
  Use `errors.New` for static errors and `fmt.Errorf` when formatting or wrapping.
  Document exported behavior and explain non-obvious constraints in comments.
- Keep imports grouped as standard library and non-standard library, then run
  `gofmt`. Apply the same readable layout to table-driven tests.

References:
- https://github.com/zeromicro/go-zero/blob/master/rest/server.go
- https://github.com/zeromicro/go-zero/blob/master/core/collection/cache.go
