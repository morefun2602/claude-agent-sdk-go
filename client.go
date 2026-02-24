package claude_agent_sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
)

// Client provides bidirectional, stateful conversations with Claude Code.
//
// Key features:
//   - Bidirectional: Send and receive messages at any time
//   - Stateful: Maintains conversation context across messages
//   - Interactive: Send follow-ups based on responses
//   - Control flow: Support for interrupts and session management
//
// For simple one-shot queries, use the Query() function instead.
//
// Usage:
//
//	client := claude_agent_sdk.NewClient(opts)
//	err := client.Connect(ctx)
//	defer client.Disconnect()
//
//	err = client.SendQuery(ctx, "Hello", "default")
//	msgCh, errCh := client.ReceiveResponse(ctx)
//	for msg := range msgCh { ... }
type Client struct {
	Options          *ClaudeAgentOptions
	customTransport  Transport
	transport        Transport
	handler          *queryHandler
}

// NewClient creates a new Claude SDK client.
func NewClient(options *ClaudeAgentOptions, transport ...Transport) *Client {
	if options == nil {
		options = &ClaudeAgentOptions{}
	}

	c := &Client{Options: options}
	if len(transport) > 0 && transport[0] != nil {
		c.customTransport = transport[0]
	}

	os.Setenv("CLAUDE_CODE_ENTRYPOINT", "sdk-go-client")
	return c
}

// Connect establishes a connection to Claude Code.
// If prompt is non-empty, it is sent as the initial message. If streamCh is
// provided, messages from that channel are streamed to the CLI.
func (c *Client) Connect(ctx context.Context) error {
	return c.ConnectWithPrompt(ctx, nil, nil)
}

// ConnectWithPrompt connects with an optional initial prompt string or stream channel.
func (c *Client) ConnectWithPrompt(ctx context.Context, prompt *string, streamCh <-chan map[string]interface{}) error {
	configuredOptions := *c.Options
	if c.Options.CanUseTool != nil {
		if prompt != nil {
			return fmt.Errorf("can_use_tool callback requires streaming mode; use streamCh instead of prompt")
		}
		if c.Options.PermissionPromptToolName != nil {
			return fmt.Errorf("can_use_tool callback cannot be used with PermissionPromptToolName")
		}
		stdio := "stdio"
		configuredOptions.PermissionPromptToolName = &stdio
	}

	if c.customTransport != nil {
		c.transport = c.customTransport
	} else {
		t, err := NewSubprocessCLITransport(&configuredOptions)
		if err != nil {
			return err
		}
		c.transport = t
	}

	if err := c.transport.Connect(ctx); err != nil {
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

	c.handler = newQueryHandler(queryHandlerConfig{
		transport:         c.transport,
		canUseTool:        configuredOptions.CanUseTool,
		hooks:             configuredOptions.Hooks,
		sdkMcpServers:     sdkMcpServers,
		agents:            agentsDict,
		initializeTimeout: initTimeout,
	})

	c.handler.start(ctx)

	if _, err := c.handler.initialize(ctx); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}

	if streamCh != nil {
		go c.handler.streamInput(ctx, streamCh)
	}

	return nil
}

// SendQuery sends a new message in the conversation.
func (c *Client) SendQuery(ctx context.Context, prompt string, sessionID string) error {
	if c.handler == nil || c.transport == nil {
		return &CLIConnectionError{Msg: "not connected; call Connect() first"}
	}

	if sessionID == "" {
		sessionID = "default"
	}

	message := map[string]interface{}{
		"type":               "user",
		"message":            map[string]interface{}{"role": "user", "content": prompt},
		"parent_tool_use_id": nil,
		"session_id":         sessionID,
	}

	b, _ := json.Marshal(message)
	return c.transport.Write(ctx, string(b)+"\n")
}

// SendStreamMessage sends a raw message dict in streaming mode.
func (c *Client) SendStreamMessage(ctx context.Context, msg map[string]interface{}) error {
	if c.handler == nil || c.transport == nil {
		return &CLIConnectionError{Msg: "not connected; call Connect() first"}
	}

	b, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}
	return c.transport.Write(ctx, string(b)+"\n")
}

// ReceiveMessages returns a channel of all messages from Claude.
// The message channel is closed when no more messages are available.
// The error channel receives at most one error.
func (c *Client) ReceiveMessages(ctx context.Context) (<-chan Message, <-chan error) {
	msgCh := make(chan Message, 100)
	errCh := make(chan error, 1)

	if c.handler == nil {
		errCh <- &CLIConnectionError{Msg: "not connected; call Connect() first"}
		close(msgCh)
		close(errCh)
		return msgCh, errCh
	}

	go func() {
		defer close(msgCh)
		defer close(errCh)

		for raw := range c.handler.receiveMessages() {
			msgType, _ := raw["type"].(string)
			if msgType == "end" {
				break
			}
			if msgType == "error" {
				errMsg, _ := raw["error"].(string)
				errCh <- fmt.Errorf("stream error: %s", errMsg)
				return
			}

			msg, err := ParseMessage(raw)
			if err != nil {
				continue
			}

			select {
			case msgCh <- msg:
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			}
		}
	}()

	return msgCh, errCh
}

// ReceiveResponse receives messages until and including a ResultMessage.
// The ResultMessage IS included in the yielded messages. The channel is closed
// after the ResultMessage is sent.
func (c *Client) ReceiveResponse(ctx context.Context) (<-chan Message, <-chan error) {
	msgCh := make(chan Message, 100)
	errCh := make(chan error, 1)

	if c.handler == nil {
		errCh <- &CLIConnectionError{Msg: "not connected; call Connect() first"}
		close(msgCh)
		close(errCh)
		return msgCh, errCh
	}

	go func() {
		defer close(msgCh)
		defer close(errCh)

		for raw := range c.handler.receiveMessages() {
			msgType, _ := raw["type"].(string)
			if msgType == "end" {
				break
			}
			if msgType == "error" {
				errMsg, _ := raw["error"].(string)
				errCh <- fmt.Errorf("stream error: %s", errMsg)
				return
			}

			msg, err := ParseMessage(raw)
			if err != nil {
				continue
			}

			select {
			case msgCh <- msg:
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			}

			if _, ok := msg.(*ResultMessage); ok {
				return
			}
		}
	}()

	return msgCh, errCh
}

// Interrupt sends an interrupt signal to Claude.
func (c *Client) Interrupt(ctx context.Context) error {
	if c.handler == nil {
		return &CLIConnectionError{Msg: "not connected; call Connect() first"}
	}
	return c.handler.interrupt(ctx)
}

// SetPermissionMode changes the permission mode during a conversation.
func (c *Client) SetPermissionMode(ctx context.Context, mode string) error {
	if c.handler == nil {
		return &CLIConnectionError{Msg: "not connected; call Connect() first"}
	}
	return c.handler.setPermissionMode(ctx, mode)
}

// SetModel changes the AI model during a conversation.
func (c *Client) SetModel(ctx context.Context, model *string) error {
	if c.handler == nil {
		return &CLIConnectionError{Msg: "not connected; call Connect() first"}
	}
	return c.handler.setModel(ctx, model)
}

// RewindFiles rewinds tracked files to their state at a specific user message.
// Requires EnableFileCheckpointing to be set in options.
func (c *Client) RewindFiles(ctx context.Context, userMessageID string) error {
	if c.handler == nil {
		return &CLIConnectionError{Msg: "not connected; call Connect() first"}
	}
	return c.handler.rewindFiles(ctx, userMessageID)
}

// GetMcpStatus returns the current MCP server connection status.
func (c *Client) GetMcpStatus(ctx context.Context) (map[string]interface{}, error) {
	if c.handler == nil {
		return nil, &CLIConnectionError{Msg: "not connected; call Connect() first"}
	}
	return c.handler.getMcpStatus(ctx)
}

// GetServerInfo returns server initialization info obtained during Connect.
func (c *Client) GetServerInfo() map[string]interface{} {
	if c.handler == nil {
		return nil
	}
	return c.handler.initResult
}

// Disconnect closes the connection and releases all resources.
func (c *Client) Disconnect() error {
	if c.handler != nil {
		c.handler.close()
		c.handler = nil
	}
	c.transport = nil
	return nil
}
