package read

import (
	"context"
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

	sb := &testSandboxer{allowReadFn: func(p string) bool { return false }}
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

	sb := &testSandboxer{allowReadFn: func(p string) bool { return true }}
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

type testSandboxer struct {
	allowReadFn  func(string) bool
	allowWriteFn func(string) bool
	wrapFn       func(cmd, dir string) (string, error)
}

func (ts *testSandboxer) WrapCommand(cmd, dir string) (string, error) {
	if ts.wrapFn != nil {
		return ts.wrapFn(cmd, dir)
	}

	return cmd, nil
}

func (ts *testSandboxer) AllowWrite(path string) bool {
	if ts.allowWriteFn != nil {
		return ts.allowWriteFn(path)
	}

	return true
}

func (ts *testSandboxer) AllowRead(path string) bool {
	if ts.allowReadFn != nil {
		return ts.allowReadFn(path)
	}

	return true
}

func (ts *testSandboxer) Mode() string   { return "auto" }
func (ts *testSandboxer) SetMode(string) {}
