package claude_agent_sdk

import "fmt"

// SDKError is the base error type for all Claude SDK errors.
type SDKError struct {
	Msg string
}

func (e *SDKError) Error() string { return e.Msg }

// CLIConnectionError is raised when unable to connect to Claude Code.
type CLIConnectionError struct {
	Msg string
}

func (e *CLIConnectionError) Error() string { return e.Msg }

// CLINotFoundError is raised when the Claude Code binary is not found.
type CLINotFoundError struct {
	Msg     string
	CLIPath string
}

func (e *CLINotFoundError) Error() string {
	if e.CLIPath != "" {
		return fmt.Sprintf("%s: %s", e.Msg, e.CLIPath)
	}
	return e.Msg
}

// ProcessError is raised when the CLI process fails.
type ProcessError struct {
	Msg      string
	ExitCode *int
	Stderr   *string
}

func (e *ProcessError) Error() string {
	msg := e.Msg
	if e.ExitCode != nil {
		msg = fmt.Sprintf("%s (exit code: %d)", msg, *e.ExitCode)
	}
	if e.Stderr != nil && *e.Stderr != "" {
		msg = fmt.Sprintf("%s\nError output: %s", msg, *e.Stderr)
	}
	return msg
}

// CLIJSONDecodeError is raised when unable to decode JSON from CLI output.
type CLIJSONDecodeError struct {
	Line     string
	Original error
}

func (e *CLIJSONDecodeError) Error() string {
	preview := e.Line
	if len(preview) > 100 {
		preview = preview[:100] + "..."
	}
	return fmt.Sprintf("Failed to decode JSON: %s", preview)
}

func (e *CLIJSONDecodeError) Unwrap() error { return e.Original }

// MessageParseError is raised when unable to parse a message from CLI output.
type MessageParseError struct {
	Msg  string
	Data map[string]interface{}
}

func (e *MessageParseError) Error() string { return e.Msg }
