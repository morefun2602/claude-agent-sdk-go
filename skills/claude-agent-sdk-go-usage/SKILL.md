---
name: claude-agent-sdk-go-usage
description: claude-agent-sdk-go SDK usage guide. Use when integrating or developing Go applications with claude-agent-sdk-go.
---

# claude-agent-sdk-go Usage Guide

## When to Use

- Integrate Claude Code CLI into Go applications
- Implement one-shot queries or bidirectional conversations
- Configure MCP servers, Hooks, and tool permissions
- Parse and process messages returned by Claude

## Prerequisites

- Install Claude Code CLI: `npm install -g @anthropic-ai/claude-code`
- Minimum version: 2.0.0
- Import: `import "github.com/morefun2602/claude-agent-sdk-go"`

---

## 1. Usage Modes Overview

The SDK provides two main modes:

| Mode | Use Case | Entry Point |
|------|----------|-------------|
| **Query** | Simple, stateless single requests | `Query(ctx, QueryInput{...})` |
| **Client** | Interactive, stateful bidirectional conversations | `NewClient(opts)` + `Connect` + `SendQuery` + `ReceiveResponse` |

---

## 2. Query (One-Shot)

For simple requests that do not require bidirectional communication.

### Basic Usage

```go
ctx := context.Background()
msgCh, errCh := claude_agent_sdk.Query(ctx, claude_agent_sdk.QueryInput{
    Prompt:  "Hello, please introduce yourself",
    Options: &claude_agent_sdk.ClaudeAgentOptions{},
})

for msg := range msgCh {
    switch m := msg.(type) {
    case *claude_agent_sdk.AssistantMessage:
        for _, block := range m.Content {
            if tb, ok := block.(claude_agent_sdk.TextBlock); ok {
                fmt.Print(tb.Text)
            }
        }
    case *claude_agent_sdk.ResultMessage:
        fmt.Printf("Done, took %d ms\n", m.DurationMs)
    }
}

if err := <-errCh; err != nil {
    log.Fatal(err)
}
```

### QueryInput Fields

| Field | Type | Description |
|-------|------|-------------|
| `Prompt` | string | User input text (mutually exclusive with Stream) |
| `Stream` | `<-chan map[string]interface{}` | Streaming input channel (required when using CanUseTool) |
| `Options` | `*ClaudeAgentOptions` | Configuration options |
| `Transport` | `Transport` | Optional, custom transport implementation |

**Note**: When using the `CanUseTool` callback, you must provide `Stream` instead of `Prompt`.

---

## 3. Client (Bidirectional Session)

For multi-turn conversations, interrupts, model switching, and similar scenarios.

### Basic Flow

```go
opts := &claude_agent_sdk.ClaudeAgentOptions{}
client := claude_agent_sdk.NewClient(opts)

if err := client.Connect(ctx); err != nil {
    log.Fatal(err)
}
defer client.Disconnect()

// Send query
if err := client.SendQuery(ctx, "Reply with exactly: OK", "default"); err != nil {
    log.Fatal(err)
}

// Receive response (channel closes after ResultMessage)
msgCh, errCh := client.ReceiveResponse(ctx)
for msg := range msgCh {
    // Process message
}

if err := <-errCh; err != nil {
    log.Fatal(err)
}
```

### Client Methods

| Method | Description |
|--------|-------------|
| `Connect(ctx)` | Establish connection |
| `ConnectWithPrompt(ctx, prompt, streamCh)` | Connect with optional initial prompt or streaming input |
| `SendQuery(ctx, prompt, sessionID)` | Send message |
| `SendStreamMessage(ctx, msg)` | Send raw streaming message |
| `ReceiveMessages(ctx)` | Receive all messages (until end) |
| `ReceiveResponse(ctx)` | Receive messages until ResultMessage is included |
| `Interrupt(ctx)` | Interrupt current generation |
| `SetPermissionMode(ctx, mode)` | Change permission mode |
| `SetModel(ctx, model)` | Switch model |
| `RewindFiles(ctx, userMessageID)` | Rewind files (requires EnableFileCheckpointing) |
| `GetMcpStatus(ctx)` | Get MCP status |
| `GetServerInfo()` | Get initialization info |
| `Disconnect()` | Disconnect |

---

## 4. Configuration Options (ClaudeAgentOptions)

### Common Fields

```go
// Helper: take pointer (Go 1.18+)
func ptr[T any](v T) *T { return &v }

opts := &claude_agent_sdk.ClaudeAgentOptions{
    Model:        ptr("claude-sonnet-4-5"),
    MaxTurns:     ptr(10),
    MaxBudgetUsd: ptr(0.5),
    Cwd:          ptr("/path/to/project"),
    CLIPath:      ptr("/path/to/claude"),
    Env:          map[string]string{"ANTHROPIC_API_KEY": "sk-..."},
}
```

### Main Options

| Field | Type | Description |
|-------|------|-------------|
| `Tools` | `*Tools` | Tool list or preset |
| `SystemPrompt` | `*SystemPrompt` | System prompt (text or preset) |
| `McpServers` | `map[string]McpServerConfig` | MCP servers |
| `PermissionMode` | `*PermissionMode` | default / acceptEdits / plan / bypassPermissions |
| `Model` | `*string` | Primary model |
| `FallbackModel` | `*string` | Fallback model |
| `MaxTurns` | `*int` | Maximum turns |
| `MaxBudgetUsd` | `*float64` | Budget cap (USD) |
| `CanUseTool` | `CanUseToolFunc` | Tool permission callback |
| `Hooks` | `map[HookEvent][]HookMatcher` | Event hooks |
| `Agents` | `map[string]AgentDefinition` | Agent definitions |
| `Cwd` | `*string` | Working directory |
| `CLIPath` | `*string` | Claude CLI path |
| `Env` | `map[string]string` | Environment variables |
| `EnableFileCheckpointing` | bool | Enable file checkpointing |

---

## 5. Message Types and Parsing

### ParseMessage

```go
msg, err := claude_agent_sdk.ParseMessage(rawJSON)
if err != nil {
    // Handle MessageParseError
}
```

### Message Types

| Type | Description |
|------|-------------|
| `*UserMessage` | User message |
| `*AssistantMessage` | Assistant reply |
| `*SystemMessage` | System message |
| `*ResultMessage` | Result (includes duration_ms, session_id, total_cost_usd, etc.) |
| `*StreamEvent` | Stream event |

### Message Type Definitions

All message types implement the `Message` interface. Use type assertion to access concrete fields:

```go
msg, _ := claude_agent_sdk.ParseMessage(raw)
switch m := msg.(type) {
case *claude_agent_sdk.UserMessage:
    // m.Content, m.UUID, m.ParentToolUseID, m.ToolUseResult
case *claude_agent_sdk.AssistantMessage:
    // m.Content, m.Model, m.ParentToolUseID, m.Error
case *claude_agent_sdk.ResultMessage:
    // m.Subtype, m.DurationMs, m.SessionID, m.TotalCostUsd, etc.
// ...
}
```

#### UserMessage

Represents a message from the user. Raw JSON `type` is `"user"`.

| Field | Type | Description |
|-------|------|-------------|
| `Content` | `interface{}` | Message content: either a `string` (plain text) or `[]ContentBlock` (multimodal) |
| `UUID` | `*string` | Optional unique identifier for the message |
| `ParentToolUseID` | `*string` | If this is a tool result, the ID of the tool use being responded to |
| `ToolUseResult` | `map[string]interface{}` | Optional tool use result data |

#### AssistantMessage

Represents a reply from the assistant. Raw JSON `type` is `"assistant"`.

| Field | Type | Description |
|-------|------|-------------|
| `Content` | `[]ContentBlock` | Array of content blocks (TextBlock, ThinkingBlock, ToolUseBlock, ToolResultBlock) |
| `Model` | `string` | Model identifier (e.g., "claude-sonnet-4-5") |
| `ParentToolUseID` | `*string` | If nested in a tool call, the parent tool use ID |
| `Error` | `*AssistantMessageError` | Set when the API returned an error instead of content |

**AssistantMessageError** constants (when `Error` is non-nil):
- `authentication_failed` — API auth failed
- `billing_error` — Billing/credits issue
- `rate_limit` — Rate limit exceeded
- `invalid_request` — Invalid request
- `server_error` — Server-side error
- `unknown` — Unknown error

#### SystemMessage

Represents a system-level message (notifications, status updates). Raw JSON `type` is `"system"`.

| Field | Type | Description |
|-------|------|-------------|
| `Subtype` | `string` | Message subtype (e.g., "notification") |
| `Data` | `map[string]interface{}` | Additional data; structure depends on subtype |

#### ResultMessage

Signals the end of a response stream. Raw JSON `type` is `"result"`. Channel typically closes after this.

| Field | Type | Description |
|-------|------|-------------|
| `Subtype` | `string` | Result subtype (e.g., "done") |
| `DurationMs` | `int` | Total wall-clock duration in milliseconds |
| `DurationApiMs` | `int` | API call duration in milliseconds |
| `IsError` | `bool` | Whether the run ended in error |
| `NumTurns` | `int` | Number of conversation turns |
| `SessionID` | `string` | Session identifier |
| `TotalCostUsd` | `*float64` | Total cost in USD (if available) |
| `Usage` | `map[string]interface{}` | Token usage stats |
| `Result` | `*string` | Optional result summary |
| `StructuredOutput` | `interface{}` | Structured output if JSON schema was requested |

#### StreamEvent

Represents a streaming event (e.g., message_start, content_block_delta). Raw JSON `type` is `"stream_event"`.

| Field | Type | Description |
|-------|------|-------------|
| `UUID` | `string` | Event UUID |
| `SessionID` | `string` | Session identifier |
| `Event` | `map[string]interface{}` | Event payload; structure varies by event type |
| `ParentToolUseID` | `*string` | Parent tool use ID if nested |

---

### Content Blocks (ContentBlock)

`AssistantMessage.Content` is `[]ContentBlock`. Each block implements `ContentBlock`; use type assertion to access fields.

#### TextBlock

Plain text output from the assistant.

| Field | Type | Description |
|-------|------|-------------|
| `Text` | `string` | The text content |

#### ThinkingBlock

Extended thinking/reasoning (when thinking is enabled).

| Field | Type | Description |
|-------|------|-------------|
| `Thinking` | `string` | The thinking content |
| `Signature` | `string` | Signature for verification |

#### ToolUseBlock

A tool invocation by the assistant.

| Field | Type | Description |
|-------|------|-------------|
| `ID` | `string` | Unique ID for this tool use (used to match ToolResultBlock) |
| `Name` | `string` | Tool name |
| `Input` | `map[string]interface{}` | Tool arguments |

#### ToolResultBlock

Result returned from a tool execution (from user or MCP).

| Field | Type | Description |
|-------|------|-------------|
| `ToolUseID` | `string` | ID of the ToolUseBlock this result corresponds to |
| `Content` | `interface{}` | Tool result content |
| `IsError` | `*bool` | True if the tool returned an error |

### Processing Example

```go
for _, block := range am.Content {
    switch b := block.(type) {
    case claude_agent_sdk.TextBlock:
        fmt.Print(b.Text)
    case claude_agent_sdk.ToolUseBlock:
        fmt.Printf("Tool: %s\n", b.Name)
    }
}
```

---

## 6. SDK MCP Server

In-process MCP server, no external process required.

### Create Tool

```go
tool := claude_agent_sdk.NewSdkMcpTool(
    "add",
    "Add two numbers",
    map[string]interface{}{
        "type": "object",
        "properties": map[string]interface{}{
            "a": map[string]interface{}{"type": "number"},
            "b": map[string]interface{}{"type": "number"},
        },
    },
    func(ctx context.Context, args map[string]interface{}) (map[string]interface{}, error) {
        a, _ := args["a"].(float64)
        b, _ := args["b"].(float64)
        return map[string]interface{}{
            "content": []map[string]interface{}{
                {"type": "text", "text": fmt.Sprintf("%.0f", a+b)},
            },
        }, nil
    },
)
```

### Register Server

```go
server := claude_agent_sdk.CreateSdkMcpServer("calc", "1.0.0", []claude_agent_sdk.SdkMcpTool{tool})

opts := &claude_agent_sdk.ClaudeAgentOptions{
    McpServers: map[string]claude_agent_sdk.McpServerConfig{
        "calc": server,
    },
}
```

---

## 7. Tool Permissions (CanUseTool)

Permission decision before tool execution.

```go
opts := &claude_agent_sdk.ClaudeAgentOptions{
    CanUseTool: func(ctx context.Context, toolName string, input map[string]interface{}, permCtx claude_agent_sdk.ToolPermissionContext) (claude_agent_sdk.PermissionResult, error) {
        // Allow
        return claude_agent_sdk.PermissionResultAllow{
            Behavior: "allow",
        }, nil
        // Or deny
        // return claude_agent_sdk.PermissionResultDeny{
        //     Behavior: "deny",
        //     Message:  "Execution denied",
        // }, nil
    },
}
```

**Note**: When using CanUseTool, Query must use `Stream` input; Client must use `ConnectWithPrompt(ctx, nil, streamCh)`.

---

## 8. Hooks

Invoke callbacks on specific events.

```go
opts := &claude_agent_sdk.ClaudeAgentOptions{
    Hooks: map[claude_agent_sdk.HookEvent][]claude_agent_sdk.HookMatcher{
        claude_agent_sdk.HookEventPreToolUse: {
            {
                Matcher: ptr("read_file"),
                Hooks: []claude_agent_sdk.HookCallbackFunc{
                    func(ctx context.Context, input map[string]interface{}, toolUseID *string) (map[string]interface{}, error) {
                        // Return map to CLI
                        return map[string]interface{}{"continue": true}, nil
                    },
                },
            },
        },
    },
}
```

### Available Events

| Event | Description |
|-------|-------------|
| `HookEventPreToolUse` | Before tool use |
| `HookEventPostToolUse` | After tool use |
| `HookEventPostToolUseFailure` | After tool use failure |
| `HookEventUserPromptSubmit` | User prompt submit |
| `HookEventStop` | Stop |
| `HookEventSubagentStop` | Subagent stop |
| `HookEventPreCompact` | Before compact |
| `HookEventNotification` | Notification |
| `HookEventSubagentStart` | Subagent start |
| `HookEventPermissionRequest` | Permission request |

---

## 9. Error Types

| Type | Description |
|------|-------------|
| `*CLINotFoundError` | CLI not found |
| `*CLIConnectionError` | Connection failed |
| `*ProcessError` | Process exited abnormally |
| `*CLIJSONDecodeError` | JSON decode failed |
| `*MessageParseError` | Message parse failed |

```go
var connErr *claude_agent_sdk.CLIConnectionError
if errors.As(err, &connErr) {
    // Connection error
}
var parseErr *claude_agent_sdk.MessageParseError
if errors.As(err, &parseErr) {
    // Message parse error
}
```

---

## 10. Custom Transport

Implement the `Transport` interface to replace the default subprocess:

```go
type Transport interface {
    Connect(ctx context.Context) error
    Write(ctx context.Context, data string) error
    ReadMessages(ctx context.Context) (<-chan map[string]interface{}, <-chan error)
    Close() error
    IsReady() bool
    EndInput() error
}
```

Usage:

```go
transport := NewMyTransport()
client := claude_agent_sdk.NewClient(opts, transport)
```

---

## 11. Environment Variables

| Variable | Description |
|----------|-------------|
| `CLAUDE_CODE_ENTRYPOINT` | Set by SDK (sdk-go / sdk-go-client) |
| `CLAUDE_CODE_STREAM_CLOSE_TIMEOUT` | Stream close timeout (milliseconds) |
| `CLAUDE_AGENT_SDK_SKIP_VERSION_CHECK` | Skip CLI version check |
| `CLAUDE_CODE_ENABLE_SDK_FILE_CHECKPOINTING` | Enable file checkpointing |
