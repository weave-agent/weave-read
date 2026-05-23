package read

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/weave-agent/weave/sdk"
	"github.com/weave-agent/weave/utils/truncate"
)

// maxLineContentBytes caps raw line content so the formatted line (with line
// number prefix and optional truncation suffix) stays under truncate.DefaultMaxBytes.
const maxLineContentBytes = truncate.DefaultMaxBytes - 100

// ParamPath is the tool parameter name for the file path.
const ParamPath = "path"

type tool struct{}

var (
	sandboxerMu sync.RWMutex
	sandboxer   sdk.Sandboxer
	guardianMu  sync.RWMutex
	guardian    sdk.Guardian
	requestSeq  atomic.Uint64
)

func setSandboxer(s sdk.Sandboxer) {
	sandboxerMu.Lock()
	sandboxer = s
	sandboxerMu.Unlock()
}

func getSandboxer() sdk.Sandboxer {
	sandboxerMu.RLock()

	s := sandboxer

	sandboxerMu.RUnlock()

	return s
}

func setGuardian(g sdk.Guardian) {
	guardianMu.Lock()
	guardian = g
	guardianMu.Unlock()
}

func getGuardian() sdk.Guardian {
	guardianMu.RLock()

	g := guardian

	guardianMu.RUnlock()

	return g
}

func init() { //nolint:gochecknoinits // extensions register tools and bus listeners during package load.
	sdk.OnBusReady(func(bus sdk.Bus) {
		bus.On(sdk.GuardianRegisteredTopic, func(ev sdk.Event) error {
			if g, ok := ev.Payload.(sdk.Guardian); ok {
				setGuardian(g)
			}

			return nil
		})

		bus.On(sdk.SandboxRegisteredTopic, func(ev sdk.Event) error {
			if s, ok := ev.Payload.(sdk.Sandboxer); ok {
				setSandboxer(s)
			}

			return nil
		})
	})

	sdk.RegisterTool[struct{}]("read", func(_ sdk.Config, _ sdk.PreferenceReader, _ struct{}) (sdk.Tool, error) {
		return &tool{}, nil
	})
}

// readLine reads one line from r, returning at most maxBytes of content.
// If the line exceeds maxBytes the excess is consumed but discarded and
// truncated is true.
func readLine(r *bufio.Reader, maxBytes int) (line string, truncated bool, err error) {
	var buf strings.Builder

	for {
		chunk, sliceErr := r.ReadSlice('\n')
		if !truncated && len(chunk) > 0 {
			if buf.Len()+len(chunk) > maxBytes {
				n := maxBytes - buf.Len()
				if n > 0 {
					buf.Write(chunk[:n])
				}

				truncated = true
			} else {
				buf.Write(chunk)
			}
		}

		if sliceErr == nil {
			return buf.String(), truncated, nil
		}

		if errors.Is(sliceErr, bufio.ErrBufferFull) {
			continue
		}

		return buf.String(), truncated, sliceErr
	}
}

func (t *tool) Name() string { return "read" }

func (t *tool) Definition() sdk.ToolDef {
	return sdk.ToolDef{
		Name:        "read",
		Description: "Read the contents of a file with optional line-based pagination.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				ParamPath: map[string]any{
					"type":        "string",
					"description": "The absolute path to the file to read.",
				},
				"offset": map[string]any{
					"type":        "number",
					"description": "The line number to start reading from (1-based). Defaults to 1.",
				},
				"limit": map[string]any{
					"type":        "number",
					"description": "Maximum number of lines to read. Defaults to all lines.",
				},
			},
			"required":             []string{ParamPath},
			"additionalProperties": false,
		},
	}
}

func parsePagination(args map[string]any) (offset, limit int) {
	offset = 1

	if v, ok := args["offset"]; ok {
		if val, ok := v.(float64); ok && val >= 1 {
			offset = int(val)
		}
	}

	if v, ok := args["limit"]; ok {
		if val, ok := v.(float64); ok && val > 0 {
			limit = int(val)
		}
	}

	return offset, limit
}

func newRequestID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err == nil {
		return prefix + "-" + hex.EncodeToString(b[:])
	}

	return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UnixNano(), requestSeq.Add(1))
}

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

func checkGuardian(ctx context.Context, path string) (sdk.GuardianRequest, *sdk.ToolResult) {
	req := guardianRequest(path)

	g := getGuardian()
	if g == nil {
		return req, nil
	}

	decision, err := g.Decide(ctx, req)
	if err != nil {
		return req, &sdk.ToolResult{Content: "guardian: " + err.Error(), IsError: true}
	}

	switch decision.Action {
	case sdk.GuardianDecisionAllow:
		return req, nil
	case sdk.GuardianDecisionBlock:
		return req, &sdk.ToolResult{Content: formatGuardianBlock(req, decision), IsError: true}
	default:
		decision.Action = sdk.GuardianDecisionBlock
		if decision.Reason == "" {
			decision.Reason = "guardian returned unresolved approval decision"
		}

		return req, &sdk.ToolResult{Content: formatGuardianBlock(req, decision), IsError: true}
	}
}

func formatGuardianBlock(req sdk.GuardianRequest, decision sdk.GuardianDecision) string {
	var b strings.Builder

	b.WriteString("guardian: blocked")
	b.WriteString("\naction: ")
	b.WriteString(string(req.Action))

	rule := decision.Profile
	if rule == "" {
		rule = decision.MatchedGrantID
	}

	if rule == "" {
		rule = decision.ID
	}

	if rule != "" {
		b.WriteString("\nrule: ")
		b.WriteString(rule)
	}

	if decision.Reason != "" {
		b.WriteString("\nreason: ")
		b.WriteString(decision.Reason)
	}

	return b.String()
}

func checkSandboxRead(ctx context.Context, s sdk.Sandboxer, path, guardianRequestID string) *sdk.ToolResult {
	if s == nil {
		return nil
	}

	expansion, err := s.RequestExpansion(ctx, sdk.SandboxExpansionRequest{
		ID:      newRequestID("read-sandbox"),
		Command: "read",
		Reason:  "Read file content",
		Filesystem: []sdk.SandboxFilesystemExpansion{
			{Path: path, Access: sdk.SandboxFilesystemRead},
		},
		Metadata: map[string]any{
			"operation":           "read",
			"guardian_request_id": guardianRequestID,
		},
	})
	if err != nil {
		return &sdk.ToolResult{Content: "sandbox expansion: " + err.Error(), IsError: true}
	}

	if expansion.State == sdk.SandboxExpansionAllowed {
		return nil
	}

	reason := expansion.Reason
	if reason == "" && expansion.Resolution != nil {
		reason = expansion.Resolution.Reason
	}

	if reason == "" && expansion.State != "" {
		reason = "sandbox expansion " + string(expansion.State)
	}

	if reason == "" {
		reason = "path is protected"
	}

	return &sdk.ToolResult{Content: "sandbox: read denied — " + reason, IsError: true}
}

func effectivePath(path string) (string, error) {
	selectedPath := path

	if _, err := os.Stat(path); err != nil {
		normalizedPath := normalizeMacOSPath(path)
		if normalizedPath != path {
			if _, normalizedErr := os.Stat(normalizedPath); normalizedErr == nil {
				selectedPath = normalizedPath
			}
		}
	}

	absPath, err := filepath.Abs(selectedPath)
	if err != nil {
		return "", fmt.Errorf("resolve effective path: %w", err)
	}

	cleanPath := filepath.Clean(absPath)

	resolvedPath, err := filepath.EvalSymlinks(cleanPath)
	if err == nil {
		return resolvedPath, nil
	}

	return cleanPath, nil
}

func (t *tool) Execute(ctx context.Context, args map[string]any) (sdk.ToolResult, error) {
	path, _ := args[ParamPath].(string)
	if path == "" {
		return sdk.ToolResult{Content: "error: path is required", IsError: true}, nil
	}

	path, err := effectivePath(path)
	if err != nil {
		return sdk.ToolResult{Content: fmt.Sprintf("error: %s", err), IsError: true}, nil
	}

	guardianReq, guardianErr := checkGuardian(ctx, path)
	if guardianErr != nil {
		return *guardianErr, nil
	}

	info, err := os.Stat(path)
	if err != nil {
		return sdk.ToolResult{Content: fmt.Sprintf("error: %s", err), IsError: true}, nil
	}

	if sandboxErr := checkSandboxRead(ctx, getSandboxer(), path, guardianReq.ID); sandboxErr != nil {
		return *sandboxErr, nil
	}

	if info.IsDir() {
		return sdk.ToolResult{Content: fmt.Sprintf("error: %s is a directory", path), IsError: true}, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return sdk.ToolResult{Content: fmt.Sprintf("error: %s", err), IsError: true}, nil
	}
	defer f.Close()

	offset, limit := parsePagination(args)

	reader := bufio.NewReader(f)

	lines, err := readLines(reader, offset, limit)
	if err != nil {
		return sdk.ToolResult{Content: fmt.Sprintf("error: %s", err), IsError: true}, nil
	}

	content := strings.Join(lines, "\n")
	result := truncate.Truncate(content, truncate.DefaultMaxLines, truncate.DefaultMaxBytes)

	if bus := sdk.BusFromContext(ctx); bus != nil {
		bus.Publish(sdk.NewEvent("tool.read.done", sdk.ReadDonePayload{
			Path:    path,
			ModTime: info.ModTime(),
		}))
	}

	// Record read synchronously to avoid a race where a back-to-back edit
	// checks the tracker before the async bus handler has processed the event.
	if tracker := sdk.GetFileTracker(); tracker != nil {
		tracker.RecordRead(path, info.ModTime())
	}

	return sdk.ToolResult{Content: result.Format(), IsError: false}, nil
}

// readLines reads formatted lines from r with the given offset and limit.
func readLines(r *bufio.Reader, offset, limit int) ([]string, error) {
	var (
		collected  []string
		lineNum    int
		maxLineNum int
		count      int
	)

	for {
		line, lineTruncated, readErr := readLine(r, maxLineContentBytes)

		if errors.Is(readErr, io.EOF) && line == "" {
			break
		}

		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return nil, readErr
		}

		lineNum++
		if lineNum >= offset {
			line = strings.TrimRight(line, "\r\n")
			if lineTruncated {
				line += "\n[... line truncated]"
			}

			collected = append(collected, line)
			maxLineNum = lineNum

			count++
			if limit > 0 && count >= limit {
				break
			}
		}

		if errors.Is(readErr, io.EOF) {
			break
		}
	}

	width := len(strconv.Itoa(maxLineNum))

	lines := make([]string, len(collected))
	for i, text := range collected {
		lines[i] = fmt.Sprintf("%*d | %s", width, offset+i, text)
	}

	return lines, nil
}
