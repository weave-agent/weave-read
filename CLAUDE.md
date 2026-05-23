# CLAUDE.md - weave-read

## Guardian/Sandbox Integration

- Register runtime integrations through `sdk.OnBusReady`, listening for `sdk.GuardianRegisteredTopic` and `sdk.SandboxRegisteredTopic`.
- Guardian checks run after path argument validation and before path normalization, sandbox checks, directory checks, file reads, read-done events, and file tracker writes.
- `read` uses `sdk.GuardianActionRead`; unresolved guardian decisions, including `ask`, are treated as blocks.
- Sandbox reads use `AllowReadWithMetadata` when available and include `guardian_request_id` so sandbox decisions can be correlated with guardian decisions.

## Build and Test

- Run `go test ./...` for the extension test suite.
- There is no local Makefile lint target. Use `golangci-lint run --config /Users/andrey/Projects/weave/.golangci.yml ./...` from this extension directory.

## Testing Conventions

- Tests that set package-level guardian, sandboxer, or file tracker state must clean it up with `t.Cleanup`.
- Cover guardian allow, block, error, and unresolved decision paths when changing read authorization.
- Keep coverage for guardian and sandbox ordering: guardian blocks must skip sandbox checks and all read side effects.
