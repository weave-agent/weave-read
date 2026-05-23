# Guardian Integration for read Tool

## Overview
Add guardian policy enforcement to the `read` tool so that file reads are subject to the guardian's allow/ask/block decisions. The `read` tool reads file contents — this is a `GuardianActionRead` action.

## Context
- **Tool file**: `read.go`
- **Test file**: `read_test.go`
- **Reference pattern**: `weave-bash` extension (`bash.go` lines 68-317)
- **Guardian action**: `sdk.GuardianActionRead`
- The read tool already has sandbox integration (`sandboxer.AllowRead(path)` at line 164); guardian must run *before* sandbox.
- Note: `ask` profile auto-allows reads, but custom profiles may block or log them.

## Development Approach
- **Testing approach**: Regular (code first, then tests)
- Complete each task fully before moving to the next
- Make small, focused changes
- **CRITICAL: every task MUST include new/updated tests**
- **CRITICAL: all tests must pass before starting next task**
- Run tests after each change

## Testing Strategy
- **Unit tests**: mock guardian with allow/block/ask/error decisions
- Verify guardian check runs before sandbox check
- Verify guardian block skips sandbox and returns error result
- Verify guardian allow proceeds to sandbox and read logic

## Implementation Steps

### Task 1: Add guardian infrastructure to read.go
- [ ] Add `guardianMu sync.RWMutex`, `guardian sdk.Guardian` package-level variables
- [ ] Add `setGuardian()` / `getGuardian()` helpers
- [ ] Add `GuardianRegisteredTopic` listener in `init()` alongside existing `sandbox.registered` listener
- [ ] Add `newRequestID()` helper
- [ ] Add `guardianRequest(path string) sdk.GuardianRequest` helper with `Action: sdk.GuardianActionRead`
- [ ] Add `checkGuardian()` helper (same pattern as bash)
- [ ] Add `formatGuardianBlock()` helper (same as bash)
- [ ] Call `checkGuardian()` at start of `Execute()`, before `os.Stat` and sandbox checks
- [ ] Pass `guardianReq.ID` into sandbox metadata for linkage
- [ ] Run read tests — must pass before next task

### Task 2: Add guardian tests to read_test.go
- [ ] Write `TestExecuteWithGuardian` with subtests:
  - "allow decision permits read"
  - "block decision returns guardian error"
  - "missing guardian permits read"
  - "guardian error returns tool error"
- [ ] Write `TestExecuteGuardianSandboxOrdering`:
  - "guardian allow runs before sandbox"
  - "guardian block skips sandbox"
- [ ] Add `testGuardian` mock helper
- [ ] Run read tests — must pass

### Task 3: Verify and cleanup
- [ ] Run `make lint` in read extension directory
- [ ] Run full test suite for read extension
- [ ] Verify no regressions in existing read functionality

## Technical Details

### guardianRequest for read
```go
func guardianRequest(path string) sdk.GuardianRequest {
    return sdk.GuardianRequest{
        ID:          newRequestID("read-guardian"),
        ToolName:    "read",
        Action:      sdk.GuardianActionRead,
        Path:        path,
        Description: "Read file content",
        Metadata: map[string]any{
            "operation": "read",
        },
    }
}
```

### Execute ordering
1. Validate `path` parameter
2. **Guardian check** (`checkGuardian`) — if blocked, return error
3. `os.Stat(path)` for path normalization
4. Sandbox check (`sandboxer.AllowRead`)
5. Open file, read content

## Post-Completion
- Manual verification: test read tool with `ask` profile — should auto-allow reads
- Test with custom profile that blocks reads — should block
