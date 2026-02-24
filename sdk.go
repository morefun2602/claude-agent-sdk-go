package claude_agent_sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
)

// Query performs a one-shot, unidirectional query to Claude Code.
//
// For simple, stateless queries where you don't need bidirectional communication.
// For interactive, stateful conversations, use Client instead.
//
// It returns a channel of Messages and a channel for a single error.
// The message channel is closed when all messages have been received.
func Query(ctx context.Context, input QueryInput) (<-chan Message, <-chan error) {
	msgCh := make(chan Message, 100)
	errCh := make(chan error, 1)

	go func() {
		defer close(msgCh)
		defer close(errCh)

		if err := runQuery(ctx, input, msgCh); err != nil {
			errCh <- err
		}
	}()

	return msgCh, errCh
}

func runQuery(ctx context.Context, input QueryInput, msgCh chan<- Message) error {
	options := input.Options
	if options == nil {
		options = &ClaudeAgentOptions{}
	}

	os.Setenv("CLAUDE_CODE_ENTRYPOINT", "sdk-go")

	configuredOptions := *options
	if options.CanUseTool != nil {
		if input.Prompt != "" && input.Stream == nil {
			return fmt.Errorf("can_use_tool callback requires streaming mode; provide Stream instead of Prompt")
		}
		if options.PermissionPromptToolName != nil {
			return fmt.Errorf("can_use_tool callback cannot be used with PermissionPromptToolName")
		}
		stdio := "stdio"
		configuredOptions.PermissionPromptToolName = &stdio
	}

	var transport Transport
	if input.Transport != nil {
		transport = input.Transport
	} else {
		t, err := NewSubprocessCLITransport(&configuredOptions)
		if err != nil {
			return err
		}
		transport = t
	}

	if err := transport.Connect(ctx); err != nil {
		return err
	}

	sdkMcpServers := extractSdkMcpServers(&configuredOptions)
	agentsDict := convertAgents(&configuredOptions)

	var initTimeout float64 = 60.0
	if v := os.Getenv("CLAUDE_CODE_STREAM_CLOSE_TIMEOUT"); v != "" {
		if ms, err := parseFloat(v); err == nil {
			t := ms / 1000.0
			if t > 60.0 {
				initTimeout = t
			}
		}
	}

	handler := newQueryHandler(queryHandlerConfig{
		transport:         transport,
		canUseTool:        configuredOptions.CanUseTool,
		hooks:             configuredOptions.Hooks,
		sdkMcpServers:     sdkMcpServers,
		agents:            agentsDict,
		initializeTimeout: initTimeout,
	})

	defer handler.close()

	handler.start(ctx)

	if _, err := handler.initialize(ctx); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}

	if input.Prompt != "" {
		userMessage := map[string]interface{}{
			"type":               "user",
			"session_id":         "",
			"message":            map[string]interface{}{"role": "user", "content": input.Prompt},
			"parent_tool_use_id": nil,
		}
		b, _ := json.Marshal(userMessage)
		if err := transport.Write(ctx, string(b)+"\n"); err != nil {
			return fmt.Errorf("write prompt: %w", err)
		}
		if err := transport.EndInput(); err != nil {
			return fmt.Errorf("end input: %w", err)
		}
	} else if input.Stream != nil {
		go handler.streamInput(ctx, input.Stream)
	}

	for raw := range handler.receiveMessages() {
		msgType, _ := raw["type"].(string)
		if msgType == "end" {
			break
		}
		if msgType == "error" {
			errMsg, _ := raw["error"].(string)
			return fmt.Errorf("stream error: %s", errMsg)
		}

		msg, err := ParseMessage(raw)
		if err != nil {
			continue
		}

		select {
		case msgCh <- msg:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

// --- SDK MCP Server Support ---

// CreateSdkMcpServer creates an in-process MCP server for SDK tools.
//
// Unlike external MCP servers that run as separate processes, SDK MCP servers
// run directly in your application's process, providing better performance and
// easier debugging.
func CreateSdkMcpServer(name string, version string, tools []SdkMcpTool) McpSdkServerConfig {
	if version == "" {
		version = "1.0.0"
	}

	toolMap := make(map[string]*SdkMcpTool, len(tools))
	for i := range tools {
		toolMap[tools[i].Name] = &tools[i]
	}

	instance := &McpServerInstance{
		Name:    name,
		Version: version,
		Tools:   tools,
		toolMap: toolMap,
	}

	return McpSdkServerConfig{
		Type:     "sdk",
		Name:     name,
		Instance: instance,
	}
}

// NewSdkMcpTool creates a tool definition for use with CreateSdkMcpServer.
//
// inputSchema can be a JSON Schema map or nil. The handler receives tool
// arguments and returns the MCP result (typically containing a "content" key).
func NewSdkMcpTool(
	name string,
	description string,
	inputSchema interface{},
	handler func(ctx context.Context, args map[string]interface{}) (map[string]interface{}, error),
) SdkMcpTool {
	if inputSchema == nil {
		inputSchema = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
	}
	return SdkMcpTool{
		Name:        name,
		Description: description,
		InputSchema: inputSchema,
		Handler:     handler,
	}
}

// --- Helpers ---

func extractSdkMcpServers(opts *ClaudeAgentOptions) map[string]*McpServerInstance {
	servers := map[string]*McpServerInstance{}
	if opts.McpServers == nil {
		return servers
	}
	for name, cfg := range opts.McpServers {
		if sdk, ok := cfg.(McpSdkServerConfig); ok && sdk.Instance != nil {
			servers[name] = sdk.Instance
		}
	}
	return servers
}

func convertAgents(opts *ClaudeAgentOptions) map[string]map[string]interface{} {
	if opts.Agents == nil {
		return nil
	}
	result := make(map[string]map[string]interface{}, len(opts.Agents))
	for name, agent := range opts.Agents {
		m := map[string]interface{}{
			"description": agent.Description,
			"prompt":      agent.Prompt,
		}
		if agent.Tools != nil {
			m["tools"] = agent.Tools
		}
		if agent.Model != nil {
			m["model"] = *agent.Model
		}
		result[name] = m
	}
	return result
}

func parseFloat(s string) (float64, error) {
	var f float64
	_, err := fmt.Sscanf(s, "%f", &f)
	return f, err
}
