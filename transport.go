package claude_agent_sdk

import "context"

// Transport is the abstract interface for communication with Claude Code.
//
// Implementations handle the raw I/O with a Claude process or service.
// The queryHandler builds on top of this to implement the control protocol.
type Transport interface {
	// Connect establishes the transport connection.
	// For subprocess transports, this starts the process.
	Connect(ctx context.Context) error

	// Write sends raw data (typically JSON + newline) to the transport.
	Write(ctx context.Context, data string) error

	// ReadMessages returns a channel that receives parsed JSON messages
	// from the transport. The error channel signals fatal read errors.
	// Both channels are closed when reading completes.
	ReadMessages(ctx context.Context) (<-chan map[string]interface{}, <-chan error)

	// Close shuts down the transport and releases all resources.
	Close() error

	// IsReady reports whether the transport is ready for communication.
	IsReady() bool

	// EndInput signals the end of the input stream (closes stdin for process transports).
	EndInput() error
}
