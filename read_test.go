package read

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weave-agent/weave/bus"
	"github.com/weave-agent/weave/sdk"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegister(t *testing.T) {
	tool, err := sdk.GetTool("read", nil)
	require.NoError(t, err)
	assert.Equal(t, "read", tool.Name())
}

func TestDefinition(t *testing.T) {
	tool := &tool{}
	def := tool.Definition()
	assert.Equal(t, "read", def.Name)
	assert.NotNil(t, def.Parameters)
}

func TestExecute(t *testing.T) {
	tool := &tool{}

	tmpDir := t.TempDir()

	t.Run("missing path", func(t *testing.T) {
		result, err := tool.Execute(context.Background(), map[string]any{})
		require.NoError(t, err)
		assert.True(t, result.IsError)
		assert.Contains(t, result.Content, "path is required")
	})

	t.Run("nonexistent file", func(t *testing.T) {
		result, err := tool.Execute(context.Background(), map[string]any{
			"path": filepath.Join(tmpDir, "nope.txt"),
		})
		require.NoError(t, err)
		assert.True(t, result.IsError)
		assert.Contains(t, result.Content, "error:")
	})

	t.Run("directory path", func(t *testing.T) {
		result, err := tool.Execute(context.Background(), map[string]any{
			"path": tmpDir,
		})
		require.NoError(t, err)
		assert.True(t, result.IsError)
		assert.Contains(t, result.Content, "is a directory")
	})

	t.Run("read full file", func(t *testing.T) {
		path := filepath.Join(tmpDir, "full.txt")
		content := "line one\nline two\nline three"
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

		result, err := tool.Execute(context.Background(), map[string]any{"path": path})
		require.NoError(t, err)
		assert.False(t, result.IsError)
		assert.Contains(t, result.Content, "1 | line one")
		assert.Contains(t, result.Content, "2 | line two")
		assert.Contains(t, result.Content, "3 | line three")
	})

	t.Run("read with offset", func(t *testing.T) {
		path := filepath.Join(tmpDir, "offset.txt")
		content := "first\nsecond\nthird\nfourth"
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

		result, err := tool.Execute(context.Background(), map[string]any{
			"path":   path,
			"offset": float64(3),
		})
		require.NoError(t, err)
		assert.False(t, result.IsError)
		assert.Contains(t, result.Content, "3 | third")
		assert.Contains(t, result.Content, "4 | fourth")
		assert.NotContains(t, result.Content, "1 | first")
	})

	t.Run("read with limit", func(t *testing.T) {
		path := filepath.Join(tmpDir, "limit.txt")
		content := "first\nsecond\nthird\nfourth\nfifth"
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

		result, err := tool.Execute(context.Background(), map[string]any{
			"path":  path,
			"limit": float64(2),
		})
		require.NoError(t, err)
		assert.False(t, result.IsError)
		assert.Contains(t, result.Content, "1 | first")
		assert.Contains(t, result.Content, "2 | second")
		assert.NotContains(t, result.Content, "3 | third")
	})

	t.Run("read with offset and limit", func(t *testing.T) {
		path := filepath.Join(tmpDir, "offsetlimit.txt")
		content := "a\nb\nc\nd\ne"
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

		result, err := tool.Execute(context.Background(), map[string]any{
			"path":   path,
			"offset": float64(2),
			"limit":  float64(2),
		})
		require.NoError(t, err)
		assert.False(t, result.IsError)
		assert.Contains(t, result.Content, "2 | b")
		assert.Contains(t, result.Content, "3 | c")
		assert.NotContains(t, result.Content, "1 | a")
		assert.NotContains(t, result.Content, "4 | d")
	})

	t.Run("binary file", func(t *testing.T) {
		path := filepath.Join(tmpDir, "binary.bin")
		data := []byte{0x00, 0x01, 0x02, 0xFF, 0xFE}
		require.NoError(t, os.WriteFile(path, data, 0o644))

		result, err := tool.Execute(context.Background(), map[string]any{"path": path})
		require.NoError(t, err)
		assert.False(t, result.IsError)
		assert.NotEmpty(t, result.Content)
	})

	t.Run("empty file", func(t *testing.T) {
		path := filepath.Join(tmpDir, "empty.txt")
		require.NoError(t, os.WriteFile(path, []byte(""), 0o644))

		result, err := tool.Execute(context.Background(), map[string]any{"path": path})
		require.NoError(t, err)
		assert.False(t, result.IsError)
		assert.Empty(t, result.Content)
	})

	t.Run("offset beyond file length", func(t *testing.T) {
		path := filepath.Join(tmpDir, "short.txt")
		content := "first\nsecond\nthird"
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

		result, err := tool.Execute(context.Background(), map[string]any{
			"path":   path,
			"offset": float64(100),
		})
		require.NoError(t, err)
		assert.False(t, result.IsError)
		assert.Empty(t, result.Content)
	})

	t.Run("limit of zero returns all lines", func(t *testing.T) {
		path := filepath.Join(tmpDir, "limitzero.txt")
		content := "a\nb\nc\nd\ne"
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

		result, err := tool.Execute(context.Background(), map[string]any{
			"path":  path,
			"limit": float64(0),
		})
		require.NoError(t, err)
		assert.False(t, result.IsError)
		assert.Contains(t, result.Content, "1 | a")
		assert.Contains(t, result.Content, "5 | e")
	})

	t.Run("large file truncation", func(t *testing.T) {
		path := filepath.Join(tmpDir, "large.txt")

		lines := make([]string, 3000)
		for i := range lines {
			lines[i] = strings.Repeat("x", 20)
		}

		require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644))

		result, err := tool.Execute(context.Background(), map[string]any{"path": path})
		require.NoError(t, err)
		assert.False(t, result.IsError)
		assert.Contains(t, result.Content, "output truncated")
	})

	t.Run("long line", func(t *testing.T) {
		path := filepath.Join(tmpDir, "longline.txt")
		longLine := strings.Repeat("x", 2*1024*1024)
		require.NoError(t, os.WriteFile(path, []byte("before\nTARGET"+longLine+"\nafter"), 0o644))

		result, err := tool.Execute(context.Background(), map[string]any{"path": path})
		require.NoError(t, err)
		assert.False(t, result.IsError)
		assert.Contains(t, result.Content, "1 | before")
	})

	t.Run("very long line exceeds old scanner cap", func(t *testing.T) {
		path := filepath.Join(tmpDir, "verylongline.txt")
		longLine := strings.Repeat("y", 12*1024*1024)
		require.NoError(t, os.WriteFile(path, []byte("first\n"+longLine+"\nlast"), 0o644))

		result, err := tool.Execute(context.Background(), map[string]any{"path": path})
		require.NoError(t, err)
		assert.False(t, result.IsError)
		assert.Contains(t, result.Content, "1 | first")
	})

	t.Run("single long line produces visible content", func(t *testing.T) {
		path := filepath.Join(tmpDir, "singlelongline.txt")
		longLine := strings.Repeat("a", 60000)
		require.NoError(t, os.WriteFile(path, []byte(longLine), 0o644))

		result, err := tool.Execute(context.Background(), map[string]any{"path": path})
		require.NoError(t, err)
		assert.False(t, result.IsError)
		assert.Contains(t, result.Content, "1 | ")
		assert.Contains(t, result.Content, "line truncated")
		assert.Contains(t, result.Content, "a")
	})
}

func TestExecuteSandboxDenied(t *testing.T) {
	tool := &tool{}
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.txt")
	require.NoError(t, os.WriteFile(path, []byte("secret data"), 0o644))

	sb := &testSandboxer{requestExpansionFn: func(_ context.Context, req sdk.SandboxExpansionRequest) (sdk.SandboxExpansion, error) {
		return sdk.SandboxExpansion{RequestID: req.ID, State: sdk.SandboxExpansionDenied, Reason: "path is protected"}, nil
	}}
	setSandboxer(sb)

	t.Cleanup(func() { setSandboxer(nil) })

	result, err := tool.Execute(context.Background(), map[string]any{"path": path})
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content, "sandbox: read denied")
}

func TestExecuteSandboxAllowed(t *testing.T) {
	tool := &tool{}
	dir := t.TempDir()
	path := filepath.Join(dir, "readable.txt")
	require.NoError(t, os.WriteFile(path, []byte("hello"), 0o644))

	sb := &testSandboxer{requestExpansionFn: func(_ context.Context, req sdk.SandboxExpansionRequest) (sdk.SandboxExpansion, error) {
		return sdk.SandboxExpansion{RequestID: req.ID, State: sdk.SandboxExpansionAllowed}, nil
	}}
	setSandboxer(sb)

	t.Cleanup(func() { setSandboxer(nil) })

	result, err := tool.Execute(context.Background(), map[string]any{"path": path})
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "hello")
}

func TestExecuteSandboxNil(t *testing.T) {
	tool := &tool{}
	dir := t.TempDir()
	path := filepath.Join(dir, "normal.txt")
	require.NoError(t, os.WriteFile(path, []byte("normal data"), 0o644))

	setSandboxer(nil)

	result, err := tool.Execute(context.Background(), map[string]any{"path": path})
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "normal data")
}

func TestExecuteNormalizedPath(t *testing.T) {
	tool := &tool{}
	dir := t.TempDir()

	t.Run("curly quotes normalized to straight quotes", func(t *testing.T) {
		// Create file with straight quotes on disk
		actualPath := filepath.Join(dir, `"quoted".txt`)
		require.NoError(t, os.WriteFile(actualPath, []byte("quoted content"), 0o644))

		// Try to read with curly quotes
		curlyPath := filepath.Join(dir, "“quoted”.txt")
		result, err := tool.Execute(context.Background(), map[string]any{"path": curlyPath})
		require.NoError(t, err)
		assert.False(t, result.IsError)
		assert.Contains(t, result.Content, "quoted content")
	})

	t.Run("unicode spaces normalized to regular space", func(t *testing.T) {
		// Create file with regular spaces on disk
		actualPath := filepath.Join(dir, "spaced file.txt")
		require.NoError(t, os.WriteFile(actualPath, []byte("spaced content"), 0o644))

		// Try to read with non-breaking spaces
		nbspPath := filepath.Join(dir, "spaced file.txt")
		result, err := tool.Execute(context.Background(), map[string]any{"path": nbspPath})
		require.NoError(t, err)
		assert.False(t, result.IsError)
		assert.Contains(t, result.Content, "spaced content")
	})

	t.Run("NFD normalization for unicode characters", func(t *testing.T) {
		// Create file with NFD name on disk (decomposed é)
		actualPath := filepath.Join(dir, "café.txt")
		require.NoError(t, os.WriteFile(actualPath, []byte("cafe content"), 0o644))

		// Try to read with NFC name (precomposed é)
		nfcPath := filepath.Join(dir, "café.txt")
		result, err := tool.Execute(context.Background(), map[string]any{"path": nfcPath})
		require.NoError(t, err)
		assert.False(t, result.IsError)
		assert.Contains(t, result.Content, "cafe content")
	})

	t.Run("no normalization needed passthrough", func(t *testing.T) {
		path := filepath.Join(dir, "plain.txt")
		require.NoError(t, os.WriteFile(path, []byte("plain content"), 0o644))

		result, err := tool.Execute(context.Background(), map[string]any{"path": path})
		require.NoError(t, err)
		assert.False(t, result.IsError)
		assert.Contains(t, result.Content, "plain content")
	})

	t.Run("normalization does not help nonexistent file", func(t *testing.T) {
		// A path that normalizes but still doesn't exist
		curlyPath := filepath.Join(dir, "“nonexistent”.txt")
		result, err := tool.Execute(context.Background(), map[string]any{"path": curlyPath})
		require.NoError(t, err)
		assert.True(t, result.IsError)
		assert.Contains(t, result.Content, "error:")
	})
}

func TestExecutePublishesReadDoneEvent(t *testing.T) {
	tool := &tool{}
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "event.txt")
	require.NoError(t, os.WriteFile(path, []byte("hello world"), 0o644))

	b := bus.New()

	var captured struct {
		sync.Mutex
		event sdk.Event
		got   bool
	}

	b.On("tool.read.done", func(e sdk.Event) error {
		captured.Lock()
		defer captured.Unlock()

		captured.event = e
		captured.got = true

		return nil
	})

	ctx := sdk.WithBus(context.Background(), b)
	result, err := tool.Execute(ctx, map[string]any{"path": path})
	require.NoError(t, err)
	assert.False(t, result.IsError)

	// Close waits for handlers to finish processing
	require.NoError(t, b.Close())

	captured.Lock()
	defer captured.Unlock()

	assert.True(t, captured.got, "expected tool.read.done event to be published")
	assert.Equal(t, "tool.read.done", captured.event.Topic)

	payload, ok := captured.event.Payload.(sdk.ReadDonePayload)
	require.True(t, ok, "expected payload to be ReadDonePayload")
	assert.Equal(t, path, payload.Path)
	assert.False(t, payload.ModTime.IsZero())
}

func TestExecuteNoEventOnError(t *testing.T) {
	tool := &tool{}
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "nonexistent.txt")

	b := bus.New()

	var captured struct {
		sync.Mutex
		got bool
	}

	b.On("tool.read.done", func(e sdk.Event) error {
		captured.Lock()
		defer captured.Unlock()

		captured.got = true

		return nil
	})

	ctx := sdk.WithBus(context.Background(), b)
	result, err := tool.Execute(ctx, map[string]any{"path": path})
	require.NoError(t, err)
	assert.True(t, result.IsError)

	require.NoError(t, b.Close())

	captured.Lock()
	defer captured.Unlock()

	assert.False(t, captured.got, "expected no tool.read.done event on error")
}

func TestExecuteNoEventWithoutBus(t *testing.T) {
	tool := &tool{}
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "nobus.txt")
	require.NoError(t, os.WriteFile(path, []byte("content"), 0o644))

	// No bus in context — should not panic
	result, err := tool.Execute(context.Background(), map[string]any{"path": path})
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "content")
}

func TestGuardianAndSandboxRegistration(t *testing.T) {
	origGuardian := getGuardian()
	origSandboxer := getSandboxer()

	setGuardian(nil)
	setSandboxer(nil)

	t.Cleanup(func() {
		setGuardian(origGuardian)
		setSandboxer(origSandboxer)
	})

	registrationBus := newRegistrationBus()
	sdk.InvokeBusSubscribers(registrationBus)

	g := &testGuardian{}
	s := &testSandboxer{}

	registrationBus.Publish(sdk.NewEvent(sdk.GuardianRegisteredTopic, g))
	registrationBus.Publish(sdk.NewEvent(sdk.SandboxRegisteredTopic, s))

	assert.Same(t, g, getGuardian())
	assert.Same(t, s, getSandboxer())

	registrationBus.Publish(sdk.NewEvent(sdk.GuardianRegisteredTopic, "not a guardian"))
	registrationBus.Publish(sdk.NewEvent(sdk.SandboxRegisteredTopic, "not a sandboxer"))

	assert.Same(t, g, getGuardian())
	assert.Same(t, s, getSandboxer())
}

func TestExecuteWithGuardian(t *testing.T) {
	origGuardian := getGuardian()
	origSandboxer := getSandboxer()

	setGuardian(nil)
	setSandboxer(nil)

	t.Cleanup(func() {
		setGuardian(origGuardian)
		setSandboxer(origSandboxer)
	})

	t.Run("allow decision permits read", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "allow.txt")
		require.NoError(t, os.WriteFile(path, []byte("guardian allowed"), 0o644))

		var gotReq sdk.GuardianRequest

		setGuardian(&testGuardian{
			decideFn: func(_ context.Context, req sdk.GuardianRequest) (sdk.GuardianDecision, error) {
				gotReq = req

				return sdk.GuardianDecision{
					ID:        "decision-allow",
					RequestID: req.ID,
					Action:    sdk.GuardianDecisionAllow,
				}, nil
			},
		})
		setSandboxer(nil)

		result, err := (&tool{}).Execute(context.Background(), map[string]any{"path": path})
		require.NoError(t, err)
		assert.False(t, result.IsError)
		assert.Contains(t, result.Content, "guardian allowed")

		assert.NotEmpty(t, gotReq.ID)
		assert.Equal(t, "read", gotReq.ToolName)
		assert.Equal(t, sdk.GuardianActionRead, gotReq.Action)
		assert.Equal(t, path, gotReq.Path)
		assert.Equal(t, "read", gotReq.Metadata["operation"])
	})

	t.Run("block decision returns guardian error", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "block.txt")
		require.NoError(t, os.WriteFile(path, []byte("should not be read"), 0o644))

		sandboxCalled := false

		setGuardian(&testGuardian{
			decideFn: func(_ context.Context, req sdk.GuardianRequest) (sdk.GuardianDecision, error) {
				return sdk.GuardianDecision{
					ID:        "decision-block",
					RequestID: req.ID,
					Action:    sdk.GuardianDecisionBlock,
					Reason:    "read blocked by policy",
					Profile:   "strict",
				}, nil
			},
		})
		setSandboxer(&testSandboxer{requestExpansionFn: func(_ context.Context, req sdk.SandboxExpansionRequest) (sdk.SandboxExpansion, error) {
			sandboxCalled = true

			return sdk.SandboxExpansion{RequestID: req.ID, State: sdk.SandboxExpansionAllowed}, nil
		}})

		result, err := (&tool{}).Execute(context.Background(), map[string]any{"path": path})
		require.NoError(t, err)
		assert.True(t, result.IsError)
		assert.Contains(t, result.Content, "guardian: blocked")
		assert.Contains(t, result.Content, "action: read")
		assert.Contains(t, result.Content, "rule: strict")
		assert.Contains(t, result.Content, "reason: read blocked by policy")
		assert.False(t, sandboxCalled)
	})

	t.Run("missing guardian permits read", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "missing.txt")
		require.NoError(t, os.WriteFile(path, []byte("no guardian"), 0o644))

		setGuardian(nil)
		setSandboxer(nil)

		result, err := (&tool{}).Execute(context.Background(), map[string]any{"path": path})
		require.NoError(t, err)
		assert.False(t, result.IsError)
		assert.Contains(t, result.Content, "no guardian")
	})

	t.Run("guardian error returns tool error", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "error.txt")
		require.NoError(t, os.WriteFile(path, []byte("policy unavailable"), 0o644))

		setGuardian(&testGuardian{
			decideFn: func(context.Context, sdk.GuardianRequest) (sdk.GuardianDecision, error) {
				return sdk.GuardianDecision{}, errors.New("policy engine unavailable")
			},
		})
		setSandboxer(nil)

		result, err := (&tool{}).Execute(context.Background(), map[string]any{"path": path})
		require.NoError(t, err)
		assert.True(t, result.IsError)
		assert.Contains(t, result.Content, "guardian: policy engine unavailable")
	})

	t.Run("ask decision returns unresolved guardian error", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "ask.txt")
		require.NoError(t, os.WriteFile(path, []byte("pending approval"), 0o644))

		eventBus := bus.New()
		tracker := newMockFileTracker()
		sdk.SetFileTracker(tracker)

		var eventSeen bool

		eventBus.On("tool.read.done", func(sdk.Event) error {
			eventSeen = true

			return nil
		})

		t.Cleanup(func() {
			sdk.SetFileTracker(nil)
			require.NoError(t, eventBus.Close())
		})

		sandboxCalled := false

		setGuardian(&testGuardian{
			decideFn: func(_ context.Context, req sdk.GuardianRequest) (sdk.GuardianDecision, error) {
				return sdk.GuardianDecision{
					ID:        "decision-ask",
					RequestID: req.ID,
					Action:    sdk.GuardianDecisionAsk,
				}, nil
			},
		})
		setSandboxer(&testSandboxer{requestExpansionFn: func(_ context.Context, req sdk.SandboxExpansionRequest) (sdk.SandboxExpansion, error) {
			sandboxCalled = true

			return sdk.SandboxExpansion{RequestID: req.ID, State: sdk.SandboxExpansionAllowed}, nil
		}})

		ctx := sdk.WithBus(context.Background(), eventBus)
		result, err := (&tool{}).Execute(ctx, map[string]any{"path": path})
		require.NoError(t, err)
		assert.True(t, result.IsError)
		assert.Contains(t, result.Content, "guardian: blocked")
		assert.Contains(t, result.Content, "reason: guardian returned unresolved approval decision")
		assert.False(t, sandboxCalled)
		assert.False(t, tracker.WasRead(path))
		assert.False(t, eventSeen)
	})

	t.Run("unknown decision returns unresolved guardian error", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "unknown.txt")
		require.NoError(t, os.WriteFile(path, []byte("unknown approval"), 0o644))

		eventBus := bus.New()
		tracker := newMockFileTracker()
		sdk.SetFileTracker(tracker)

		var eventSeen bool

		eventBus.On("tool.read.done", func(sdk.Event) error {
			eventSeen = true

			return nil
		})

		t.Cleanup(func() {
			sdk.SetFileTracker(nil)
			require.NoError(t, eventBus.Close())
		})

		sandboxCalled := false

		setGuardian(&testGuardian{
			decideFn: func(_ context.Context, req sdk.GuardianRequest) (sdk.GuardianDecision, error) {
				return sdk.GuardianDecision{
					ID:        "decision-unknown",
					RequestID: req.ID,
				}, nil
			},
		})
		setSandboxer(&testSandboxer{requestExpansionFn: func(_ context.Context, req sdk.SandboxExpansionRequest) (sdk.SandboxExpansion, error) {
			sandboxCalled = true

			return sdk.SandboxExpansion{RequestID: req.ID, State: sdk.SandboxExpansionAllowed}, nil
		}})

		ctx := sdk.WithBus(context.Background(), eventBus)
		result, err := (&tool{}).Execute(ctx, map[string]any{"path": path})
		require.NoError(t, err)
		assert.True(t, result.IsError)
		assert.Contains(t, result.Content, "guardian: blocked")
		assert.Contains(t, result.Content, "reason: guardian returned unresolved approval decision")
		assert.False(t, sandboxCalled)
		assert.False(t, tracker.WasRead(path))
		assert.False(t, eventSeen)
	})
}

func TestExecuteNormalizedPathWithGuardian(t *testing.T) {
	origGuardian := getGuardian()
	origSandboxer := getSandboxer()

	setGuardian(nil)
	setSandboxer(nil)

	t.Cleanup(func() {
		setGuardian(origGuardian)
		setSandboxer(origSandboxer)
	})

	dir := t.TempDir()
	actualPath := filepath.Join(dir, `"quoted".txt`)
	inputPath := filepath.Join(dir, "“quoted”.txt")

	require.NoError(t, os.WriteFile(actualPath, []byte("quoted content"), 0o644))

	var (
		guardianPath string
		sandboxPath  string
	)

	setGuardian(&testGuardian{
		decideFn: func(_ context.Context, req sdk.GuardianRequest) (sdk.GuardianDecision, error) {
			guardianPath = req.Path

			return sdk.GuardianDecision{RequestID: req.ID, Action: sdk.GuardianDecisionAllow}, nil
		},
	})
	setSandboxer(&testSandboxer{requestExpansionFn: func(_ context.Context, req sdk.SandboxExpansionRequest) (sdk.SandboxExpansion, error) {
		require.Len(t, req.Filesystem, 1)
		path := req.Filesystem[0].Path
		sandboxPath = path

		return sdk.SandboxExpansion{RequestID: req.ID, State: sdk.SandboxExpansionAllowed}, nil
	}})

	result, err := (&tool{}).Execute(context.Background(), map[string]any{"path": inputPath})
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.Contains(t, result.Content, "quoted content")
	assert.Equal(t, actualPath, guardianPath)
	assert.Equal(t, actualPath, sandboxPath)
}

func TestExecuteGuardianSandboxOrdering(t *testing.T) {
	origGuardian := getGuardian()
	origSandboxer := getSandboxer()

	setGuardian(nil)
	setSandboxer(nil)

	t.Cleanup(func() {
		setGuardian(origGuardian)
		setSandboxer(origSandboxer)
	})

	t.Run("guardian allow runs before sandbox", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "order.txt")
		require.NoError(t, os.WriteFile(path, []byte("order ok"), 0o644))

		var order []string

		setGuardian(&testGuardian{
			decideFn: func(_ context.Context, req sdk.GuardianRequest) (sdk.GuardianDecision, error) {
				order = append(order, "guardian")

				return sdk.GuardianDecision{RequestID: req.ID, Action: sdk.GuardianDecisionAllow}, nil
			},
		})
		setSandboxer(&testSandboxer{requestExpansionFn: func(_ context.Context, req sdk.SandboxExpansionRequest) (sdk.SandboxExpansion, error) {
			order = append(order, "sandbox")

			return sdk.SandboxExpansion{RequestID: req.ID, State: sdk.SandboxExpansionAllowed}, nil
		}})

		result, err := (&tool{}).Execute(context.Background(), map[string]any{"path": path})
		require.NoError(t, err)
		assert.False(t, result.IsError)
		assert.Contains(t, result.Content, "order ok")
		assert.Equal(t, []string{"guardian", "sandbox"}, order)
	})

	t.Run("guardian block skips sandbox", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "blocked.txt")
		require.NoError(t, os.WriteFile(path, []byte("blocked"), 0o644))

		var order []string

		setGuardian(&testGuardian{
			decideFn: func(_ context.Context, req sdk.GuardianRequest) (sdk.GuardianDecision, error) {
				order = append(order, "guardian")

				return sdk.GuardianDecision{
					RequestID: req.ID,
					Action:    sdk.GuardianDecisionBlock,
					Reason:    "blocked before sandbox",
				}, nil
			},
		})
		setSandboxer(&testSandboxer{requestExpansionFn: func(_ context.Context, req sdk.SandboxExpansionRequest) (sdk.SandboxExpansion, error) {
			order = append(order, "sandbox")

			return sdk.SandboxExpansion{RequestID: req.ID, State: sdk.SandboxExpansionAllowed}, nil
		}})

		result, err := (&tool{}).Execute(context.Background(), map[string]any{"path": path})
		require.NoError(t, err)
		assert.True(t, result.IsError)
		assert.Contains(t, result.Content, "reason: blocked before sandbox")
		assert.Equal(t, []string{"guardian"}, order)
	})
}

// mockFileTracker is a test-double for sdk.FileTracker.
type mockFileTracker struct {
	mu    sync.RWMutex
	reads map[string]time.Time
}

func newMockFileTracker() *mockFileTracker {
	return &mockFileTracker{
		reads: make(map[string]time.Time),
	}
}

func (m *mockFileTracker) RecordRead(path string, modTime time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.reads[path] = modTime
}

func (m *mockFileTracker) WasRead(path string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, ok := m.reads[path]

	return ok
}

func (m *mockFileTracker) GetReadTime(path string) (time.Time, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	t, ok := m.reads[path]

	return t, ok
}

func TestExecuteRecordsTrackerSynchronously(t *testing.T) {
	tool := &tool{}
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "tracker.txt")
	require.NoError(t, os.WriteFile(path, []byte("track me"), 0o644))

	tracker := newMockFileTracker()
	sdk.SetFileTracker(tracker)
	t.Cleanup(func() { sdk.SetFileTracker(nil) })

	result, err := tool.Execute(context.Background(), map[string]any{"path": path})
	require.NoError(t, err)
	assert.False(t, result.IsError)

	// Tracker must be updated synchronously before Execute returns,
	// so a back-to-back edit check will not race.
	assert.True(t, tracker.WasRead(path), "expected tracker to record read synchronously")
}

func TestGuardianRequest(t *testing.T) {
	req := guardianRequest("/tmp/readme.txt")

	assert.NotEmpty(t, req.ID)
	assert.Contains(t, req.ID, "read-guardian-")
	assert.Equal(t, "read", req.ToolName)
	assert.Equal(t, sdk.GuardianActionRead, req.Action)
	assert.Equal(t, "/tmp/readme.txt", req.Path)
	assert.Equal(t, "Read file content", req.Description)
	assert.Equal(t, "read", req.Metadata["operation"])
}

func TestCheckSandboxReadPassesGuardianMetadata(t *testing.T) {
	var gotReq sdk.SandboxExpansionRequest

	sb := &testSandboxer{requestExpansionFn: func(_ context.Context, req sdk.SandboxExpansionRequest) (sdk.SandboxExpansion, error) {
		gotReq = req

		return sdk.SandboxExpansion{RequestID: req.ID, State: sdk.SandboxExpansionAllowed}, nil
	}}

	assert.Nil(t, checkSandboxRead(context.Background(), sb, "/tmp/file.txt", "guardian-1"))
	require.Len(t, gotReq.Filesystem, 1)
	assert.Equal(t, "/tmp/file.txt", gotReq.Filesystem[0].Path)
	assert.Equal(t, sdk.SandboxFilesystemRead, gotReq.Filesystem[0].Access)
	assert.Equal(t, "read", gotReq.Metadata["operation"])
	assert.Equal(t, "guardian-1", gotReq.Metadata["guardian_request_id"])
}

type testSandboxer struct {
	requestExpansionFn func(context.Context, sdk.SandboxExpansionRequest) (sdk.SandboxExpansion, error)
}

func (ts *testSandboxer) WrapCommand(context.Context, sdk.SandboxCommandRequest) (sdk.SandboxCommand, error) {
	return sdk.SandboxCommand{}, nil
}

func (ts *testSandboxer) Status(context.Context) (sdk.SandboxStatus, error) {
	return sdk.SandboxStatus{}, nil
}

func (ts *testSandboxer) RequestExpansion(ctx context.Context, req sdk.SandboxExpansionRequest) (sdk.SandboxExpansion, error) {
	if ts.requestExpansionFn != nil {
		return ts.requestExpansionFn(ctx, req)
	}

	return sdk.SandboxExpansion{RequestID: req.ID, State: sdk.SandboxExpansionAllowed}, nil
}

func (ts *testSandboxer) ResolveExpansion(context.Context, string, sdk.SandboxExpansionResolution) error {
	return nil
}

type testGuardian struct {
	decideFn func(context.Context, sdk.GuardianRequest) (sdk.GuardianDecision, error)
}

func (tg *testGuardian) Decide(ctx context.Context, req sdk.GuardianRequest) (sdk.GuardianDecision, error) {
	if tg.decideFn == nil {
		return sdk.GuardianDecision{Action: sdk.GuardianDecisionAllow}, nil
	}

	return tg.decideFn(ctx, req)
}

func (tg *testGuardian) Resolve(context.Context, string, sdk.GuardianResolution) error {
	return nil
}

func (tg *testGuardian) Snapshot(context.Context) (sdk.GuardianSnapshot, error) {
	return sdk.GuardianSnapshot{}, nil
}

type registrationBus struct {
	handlers map[string][]sdk.Handler
}

func newRegistrationBus() *registrationBus {
	return &registrationBus{handlers: make(map[string][]sdk.Handler)}
}

func (r *registrationBus) Publish(ev sdk.Event) {
	for _, h := range r.handlers[ev.Topic] {
		_ = h(ev)
	}
}

func (r *registrationBus) On(topic string, h sdk.Handler) {
	r.handlers[topic] = append(r.handlers[topic], h)
}

func (r *registrationBus) OnAll(sdk.Handler) {}

func (r *registrationBus) Off(sdk.Handler) {}

func (r *registrationBus) Close() error { return nil }
