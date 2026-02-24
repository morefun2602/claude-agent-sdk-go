package claude_agent_sdk

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// cliAvailable returns true if Claude Code CLI is installed and executable.
func cliAvailable() bool {
	if path, err := exec.LookPath("claude"); err == nil && path != "" {
		return true
	}
	home, _ := os.UserHomeDir()
	locations := []string{
		filepath.Join(home, ".npm-global", "bin", "claude"),
		"/usr/local/bin/claude",
		filepath.Join(home, ".local", "bin", "claude"),
		filepath.Join(home, "node_modules", ".bin", "claude"),
	}
	for _, p := range locations {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

// --- Unit Tests: ParseMessage ---

func TestParseMessage_Nil(t *testing.T) {
	_, err := ParseMessage(nil)
	if err == nil {
		t.Fatal("expected error for nil input")
	}
	if _, ok := err.(*MessageParseError); !ok {
		t.Errorf("expected MessageParseError, got %T", err)
	}
}

func TestParseMessage_MissingType(t *testing.T) {
	_, err := ParseMessage(map[string]interface{}{"foo": "bar"})
	if err == nil {
		t.Fatal("expected error for missing type")
	}
}

func TestParseMessage_UserStringContent(t *testing.T) {
	data := map[string]interface{}{
		"type": "user",
		"message": map[string]interface{}{
			"role":    "user",
			"content": "Hello, Claude!",
		},
	}
	msg, err := ParseMessage(data)
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	um, ok := msg.(*UserMessage)
	if !ok {
		t.Fatalf("expected *UserMessage, got %T", msg)
	}
	if s, ok := um.Content.(string); !ok || s != "Hello, Claude!" {
		t.Errorf("content = %v (want string 'Hello, Claude!')", um.Content)
	}
}

func TestParseMessage_AssistantTextBlock(t *testing.T) {
	data := map[string]interface{}{
		"type": "assistant",
		"message": map[string]interface{}{
			"model": "claude-sonnet-4-5",
			"content": []interface{}{
				map[string]interface{}{"type": "text", "text": "Hi there!"},
			},
		},
	}
	msg, err := ParseMessage(data)
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	am, ok := msg.(*AssistantMessage)
	if !ok {
		t.Fatalf("expected *AssistantMessage, got %T", msg)
	}
	if len(am.Content) != 1 {
		t.Fatalf("len(content) = %d, want 1", len(am.Content))
	}
	tb, ok := am.Content[0].(TextBlock)
	if !ok {
		t.Fatalf("expected TextBlock, got %T", am.Content[0])
	}
	if tb.Text != "Hi there!" {
		t.Errorf("Text = %q, want %q", tb.Text, "Hi there!")
	}
}

func TestParseMessage_Result(t *testing.T) {
	data := map[string]interface{}{
		"type":            "result",
		"subtype":         "done",
		"duration_ms":     float64(1500),
		"duration_api_ms": float64(1200),
		"is_error":        false,
		"num_turns":       float64(1),
		"session_id":      "sess-123",
		"total_cost_usd":  0.002,
	}
	msg, err := ParseMessage(data)
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	rm, ok := msg.(*ResultMessage)
	if !ok {
		t.Fatalf("expected *ResultMessage, got %T", msg)
	}
	if rm.Subtype != "done" || rm.SessionID != "sess-123" || rm.NumTurns != 1 {
		t.Errorf("ResultMessage: subtype=%q session=%q turns=%d", rm.Subtype, rm.SessionID, rm.NumTurns)
	}
	if rm.TotalCostUsd == nil || *rm.TotalCostUsd != 0.002 {
		t.Errorf("TotalCostUsd = %v", rm.TotalCostUsd)
	}
}

func TestParseMessage_StreamEvent(t *testing.T) {
	data := map[string]interface{}{
		"type":       "stream_event",
		"uuid":       "evt-1",
		"session_id": "sess-1",
		"event":      map[string]interface{}{"type": "message_start"},
	}
	msg, err := ParseMessage(data)
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	se, ok := msg.(*StreamEvent)
	if !ok {
		t.Fatalf("expected *StreamEvent, got %T", msg)
	}
	if se.UUID != "evt-1" || se.SessionID != "sess-1" {
		t.Errorf("StreamEvent: uuid=%q session=%q", se.UUID, se.SessionID)
	}
}

func TestParseMessage_System(t *testing.T) {
	data := map[string]interface{}{
		"type":    "system",
		"subtype": "notification",
		"data":    map[string]interface{}{"msg": "test"},
	}
	msg, err := ParseMessage(data)
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	sm, ok := msg.(*SystemMessage)
	if !ok {
		t.Fatalf("expected *SystemMessage, got %T", msg)
	}
	if sm.Subtype != "notification" {
		t.Errorf("Subtype = %q", sm.Subtype)
	}
}

// --- Unit Tests: Errors ---

func TestErrors(t *testing.T) {
	t.Run("CLINotFoundError", func(t *testing.T) {
		e := &CLINotFoundError{Msg: "not found", CLIPath: "/bad/path"}
		s := e.Error()
		if s == "" || len(s) < 10 {
			t.Errorf("Error() = %q", s)
		}
	})
	t.Run("ProcessError", func(t *testing.T) {
		code := 1
		stderr := "something failed"
		e := &ProcessError{Msg: "failed", ExitCode: &code, Stderr: &stderr}
		s := e.Error()
		if s == "" {
			t.Error("Error() empty")
		}
	})
	t.Run("CLIJSONDecodeError", func(t *testing.T) {
		e := &CLIJSONDecodeError{Line: "invalid json", Original: nil}
		if e.Error() == "" {
			t.Error("Error() empty")
		}
	})
}

// --- Unit Tests: CreateSdkMcpServer ---

func TestCreateSdkMcpServer(t *testing.T) {
	tool := NewSdkMcpTool("add", "Add two numbers",
		map[string]interface{}{"a": "number", "b": "number"},
		func(ctx context.Context, args map[string]interface{}) (map[string]interface{}, error) {
			return map[string]interface{}{
				"content": []map[string]interface{}{{"type": "text", "text": "42"}},
			}, nil
		},
	)
	server := CreateSdkMcpServer("calc", "1.0.0", []SdkMcpTool{tool})
	if server.Type != "sdk" || server.Name != "calc" || server.Instance == nil {
		t.Errorf("CreateSdkMcpServer: type=%q name=%q instance=%v", server.Type, server.Name, server.Instance)
	}
	if len(server.Instance.Tools) != 1 || server.Instance.Tools[0].Name != "add" {
		t.Errorf("Instance.Tools = %v", server.Instance.Tools)
	}
}

// --- Unit Tests: NewSubprocessCLITransport (CLI not found) ---

func TestNewSubprocessCLITransport_CLINotFound(t *testing.T) {
	if cliAvailable() {
		t.Skip("Claude Code CLI is installed; cannot test CLINotFoundError")
	}
	_, err := NewSubprocessCLITransport(&ClaudeAgentOptions{})
	if err == nil {
		t.Fatal("expected CLINotFoundError when CLI not installed")
	}
	if _, ok := err.(*CLINotFoundError); !ok {
		t.Errorf("expected CLINotFoundError, got %T: %v", err, err)
	}
}

// --- Integration Tests (require Claude Code CLI) ---

// integrationOptions returns options with ANTHROPIC_BASE_URL pointing to local proxy (e.g. http://127.0.0.1:8080).
func integrationOptions() *ClaudeAgentOptions {
	baseURL := "http://127.0.0.1:8080"
	model := "kimi-k2.5"
	return &ClaudeAgentOptions{
		Env:   map[string]string{"ANTHROPIC_BASE_URL": baseURL},
		Model: &model,
	}
}

func TestIntegration_Query(t *testing.T) {
	if !cliAvailable() {
		t.Skip("Claude Code CLI not installed; skipping integration test")
	}
	// if os.Getenv("CLAUDE_AGENT_SDK_INTEGRATION") == "" {
	// 	t.Skip("Set CLAUDE_AGENT_SDK_INTEGRATION=1 to run integration tests")
	// }
	// }

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	msgCh, errCh := Query(ctx, QueryInput{
		Prompt:  "你好，你是谁？",
		Options: integrationOptions(),
	})

	var messages []Message
	for msg := range msgCh {
		messages = append(messages, msg)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Query error: %v", err)
		}
	default:
	}

	if len(messages) == 0 {
		t.Fatal("expected at least one message")
	}

	var gotResult bool
	for _, m := range messages {
		if _, ok := m.(*ResultMessage); ok {
			gotResult = true
			break
		}
	}
	if !gotResult {
		t.Logf("received %d messages (no ResultMessage); may be slow or API limits", len(messages))
	}
}

func TestIntegration_Client(t *testing.T) {
	if !cliAvailable() {
		t.Skip("Claude Code CLI not installed; skipping integration test")
	}
	if os.Getenv("CLAUDE_AGENT_SDK_INTEGRATION") == "" {
		t.Skip("Set CLAUDE_AGENT_SDK_INTEGRATION=1 to run integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	client := NewClient(integrationOptions())
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Disconnect()

	if err := client.SendQuery(ctx, "Reply with exactly: OK", "default"); err != nil {
		t.Fatalf("SendQuery: %v", err)
	}

	msgCh, errCh := client.ReceiveResponse(ctx)
	var count int
	for msg := range msgCh {
		count++
		_ = msg
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("ReceiveResponse error: %v", err)
		}
	default:
	}

	if count == 0 {
		t.Error("expected at least one message")
	}
}

// --- Benchmark ---

func BenchmarkParseMessage_Assistant(b *testing.B) {
	data := map[string]interface{}{
		"type": "assistant",
		"message": map[string]interface{}{
			"model": "claude-sonnet-4-5",
			"content": []interface{}{
				map[string]interface{}{"type": "text", "text": "Hello, world!"},
				map[string]interface{}{"type": "thinking", "thinking": "Let me think...", "signature": "sig"},
			},
		},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ParseMessage(data)
	}
}
