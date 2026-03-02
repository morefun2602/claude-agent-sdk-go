package claude_agent_sdk

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultMaxBufferSize       = 1024 * 1024 // 1MB
	minimumClaudeCodeVersion   = "2.0.0"
	sdkVersion                 = "0.1.0"
	versionCheckTimeoutSeconds = 2
)

// SubprocessCLITransport communicates with Claude Code via a subprocess.
type SubprocessCLITransport struct {
	options *ClaudeAgentOptions
	cliPath string
	cwd     *string

	process *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	stderr  io.ReadCloser

	ready     bool
	mu        sync.Mutex
	exitError error

	maxBufferSize int

	msgCh chan map[string]interface{}
	errCh chan error
	done  chan struct{}
}

// NewSubprocessCLITransport creates a new subprocess transport.
func NewSubprocessCLITransport(options *ClaudeAgentOptions) (*SubprocessCLITransport, error) {
	t := &SubprocessCLITransport{
		options: options,
		done:    make(chan struct{}),
	}

	if options.CLIPath != nil {
		t.cliPath = *options.CLIPath
	} else {
		path, err := t.findCLI()
		if err != nil {
			return nil, err
		}
		t.cliPath = path
	}

	if options.Cwd != nil {
		t.cwd = options.Cwd
	}

	t.maxBufferSize = defaultMaxBufferSize
	if options.MaxBufferSize != nil {
		t.maxBufferSize = *options.MaxBufferSize
	}

	return t, nil
}

func (t *SubprocessCLITransport) findCLI() (string, error) {
	if p := t.findBundledCLI(); p != "" {
		return p, nil
	}

	if path, err := exec.LookPath("claude"); err == nil {
		return path, nil
	}

	home, _ := os.UserHomeDir()
	locations := []string{
		filepath.Join(home, ".npm-global", "bin", "claude"),
		"/usr/local/bin/claude",
		filepath.Join(home, ".local", "bin", "claude"),
		filepath.Join(home, "node_modules", ".bin", "claude"),
		filepath.Join(home, ".yarn", "bin", "claude"),
		filepath.Join(home, ".claude", "local", "claude"),
	}

	for _, p := range locations {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p, nil
		}
	}

	return "", &CLINotFoundError{
		Msg: "Claude Code not found. Install with:\n" +
			"  npm install -g @anthropic-ai/claude-code\n\n" +
			"If already installed locally, try:\n" +
			"  export PATH=\"$HOME/node_modules/.bin:$PATH\"\n\n" +
			"Or provide the path via ClaudeAgentOptions:\n" +
			"  CLIPath: \"/path/to/claude\"",
	}
}

func (t *SubprocessCLITransport) findBundledCLI() string {
	cliName := "claude"
	if runtime.GOOS == "windows" {
		cliName = "claude.exe"
	}

	execPath, err := os.Executable()
	if err != nil {
		return ""
	}
	bundledPath := filepath.Join(filepath.Dir(execPath), "_bundled", cliName)
	if info, err := os.Stat(bundledPath); err == nil && !info.IsDir() {
		return bundledPath
	}
	return ""
}

func (t *SubprocessCLITransport) buildSettingsValue() *string {
	hasSettings := t.options.Settings != nil
	hasSandbox := t.options.Sandbox != nil

	if !hasSettings && !hasSandbox {
		return nil
	}

	if hasSettings && !hasSandbox {
		return t.options.Settings
	}

	settingsObj := map[string]interface{}{}
	if hasSettings {
		s := strings.TrimSpace(*t.options.Settings)
		if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") {
			_ = json.Unmarshal([]byte(s), &settingsObj)
		} else {
			data, err := os.ReadFile(s)
			if err == nil {
				_ = json.Unmarshal(data, &settingsObj)
			}
		}
	}

	if hasSandbox {
		settingsObj["sandbox"] = t.options.Sandbox
	}

	b, _ := json.Marshal(settingsObj)
	result := string(b)
	return &result
}

func (t *SubprocessCLITransport) buildCommand() []string {
	cmd := []string{t.cliPath, "--output-format", "stream-json", "--verbose"}

	if t.options.SystemPrompt == nil {
		// cmd = append(cmd, "--system-prompt", "")
	} else if t.options.SystemPrompt.Text != nil {
		cmd = append(cmd, "--system-prompt", *t.options.SystemPrompt.Text)
	} else if t.options.SystemPrompt.Preset != nil {
		p := t.options.SystemPrompt.Preset
		if p.Type == "preset" && p.Append != "" {
			cmd = append(cmd, "--append-system-prompt", p.Append)
		}
	}

	if t.options.Tools != nil {
		if t.options.Tools.Names != nil {
			if len(t.options.Tools.Names) == 0 {
				cmd = append(cmd, "--tools", "")
			} else {
				cmd = append(cmd, "--tools", strings.Join(t.options.Tools.Names, ","))
			}
		} else if t.options.Tools.Preset != nil {
			cmd = append(cmd, "--tools", "default")
		}
	}

	if len(t.options.AllowedTools) > 0 {
		cmd = append(cmd, "--allowedTools", strings.Join(t.options.AllowedTools, ","))
	}

	if t.options.MaxTurns != nil {
		cmd = append(cmd, "--max-turns", strconv.Itoa(*t.options.MaxTurns))
	}

	if t.options.MaxBudgetUsd != nil {
		cmd = append(cmd, "--max-budget-usd", fmt.Sprintf("%g", *t.options.MaxBudgetUsd))
	}

	if len(t.options.DisallowedTools) > 0 {
		cmd = append(cmd, "--disallowedTools", strings.Join(t.options.DisallowedTools, ","))
	}

	if t.options.Model != nil {
		cmd = append(cmd, "--model", *t.options.Model)
	}

	if t.options.FallbackModel != nil {
		cmd = append(cmd, "--fallback-model", *t.options.FallbackModel)
	}

	if len(t.options.Betas) > 0 {
		betas := make([]string, len(t.options.Betas))
		for i, b := range t.options.Betas {
			betas[i] = string(b)
		}
		cmd = append(cmd, "--betas", strings.Join(betas, ","))
	}

	if t.options.PermissionPromptToolName != nil {
		cmd = append(cmd, "--permission-prompt-tool", *t.options.PermissionPromptToolName)
	}

	if t.options.PermissionMode != nil {
		cmd = append(cmd, "--permission-mode", string(*t.options.PermissionMode))
	}

	if t.options.ContinueConversation {
		cmd = append(cmd, "--continue")
	}

	if t.options.Resume != nil {
		cmd = append(cmd, "--resume", *t.options.Resume)
	}

	if t.options.SessionID != nil {
		cmd = append(cmd, "--session-id", *t.options.SessionID)
	}

	if sv := t.buildSettingsValue(); sv != nil {
		cmd = append(cmd, "--settings", *sv)
	}

	for _, dir := range t.options.AddDirs {
		cmd = append(cmd, "--add-dir", dir)
	}

	if t.options.McpServers != nil {
		serversForCLI := map[string]interface{}{}
		for name, cfg := range t.options.McpServers {
			switch c := cfg.(type) {
			case McpSdkServerConfig:
				serversForCLI[name] = map[string]interface{}{
					"type": "sdk",
					"name": c.Name,
				}
			default:
				serversForCLI[name] = cfg
			}
		}
		if len(serversForCLI) > 0 {
			b, _ := json.Marshal(map[string]interface{}{"mcpServers": serversForCLI})
			cmd = append(cmd, "--mcp-config", string(b))
		}
	} else if t.options.McpServersPath != nil {
		cmd = append(cmd, "--mcp-config", *t.options.McpServersPath)
	}

	if t.options.IncludePartialMessages {
		cmd = append(cmd, "--include-partial-messages")
	}

	if t.options.ForkSession {
		cmd = append(cmd, "--fork-session")
	}

	sourcesValue := ""
	if t.options.SettingSources != nil {
		parts := make([]string, len(t.options.SettingSources))
		for i, s := range t.options.SettingSources {
			parts[i] = string(s)
		}
		sourcesValue = strings.Join(parts, ",")
	}
	cmd = append(cmd, "--setting-sources", sourcesValue)

	for _, plugin := range t.options.Plugins {
		if plugin.Type == "local" {
			cmd = append(cmd, "--plugin-dir", plugin.Path)
		}
	}

	if t.options.ExtraArgs != nil {
		for flag, value := range t.options.ExtraArgs {
			if value == nil {
				cmd = append(cmd, "--"+flag)
			} else {
				cmd = append(cmd, "--"+flag, *value)
			}
		}
	}

	resolvedMaxThinkingTokens := t.options.MaxThinkingTokens
	if t.options.Thinking != nil {
		switch t.options.Thinking.Type {
		case ThinkingAdaptive:
			if resolvedMaxThinkingTokens == nil {
				v := 32000
				resolvedMaxThinkingTokens = &v
			}
		case ThinkingEnabled:
			resolvedMaxThinkingTokens = t.options.Thinking.BudgetTokens
		case ThinkingDisabled:
			v := 0
			resolvedMaxThinkingTokens = &v
		}
	}
	if resolvedMaxThinkingTokens != nil {
		cmd = append(cmd, "--max-thinking-tokens", strconv.Itoa(*resolvedMaxThinkingTokens))
	}

	if t.options.Effort != nil {
		cmd = append(cmd, "--effort", string(*t.options.Effort))
	}

	if t.options.OutputFormat != nil {
		if tp, ok := t.options.OutputFormat["type"].(string); ok && tp == "json_schema" {
			if schema, ok := t.options.OutputFormat["schema"]; ok {
				b, _ := json.Marshal(schema)
				cmd = append(cmd, "--json-schema", string(b))
			}
		}
	}

	cmd = append(cmd, "--input-format", "stream-json")

	return cmd
}

func (t *SubprocessCLITransport) Connect(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.process != nil {
		return nil
	}

	if os.Getenv("CLAUDE_AGENT_SDK_SKIP_VERSION_CHECK") == "" {
		t.checkClaudeVersion(ctx)
	}

	cmdArgs := t.buildCommand()

	processEnv := os.Environ()
	if t.options.Env != nil {
		for k, v := range t.options.Env {
			processEnv = append(processEnv, k+"="+v)
		}
	}
	processEnv = append(processEnv,
		"CLAUDE_CODE_ENTRYPOINT=sdk-go",
		"CLAUDE_AGENT_SDK_VERSION="+sdkVersion,
	)
	if t.options.EnableFileCheckpointing {
		processEnv = append(processEnv, "CLAUDE_CODE_ENABLE_SDK_FILE_CHECKPOINTING=true")
	}

	cmd := exec.CommandContext(ctx, cmdArgs[0], cmdArgs[1:]...)
	cmd.Env = processEnv
	if t.cwd != nil {
		cmd.Dir = *t.cwd
	}

	var err error
	t.stdin, err = cmd.StdinPipe()
	if err != nil {
		return &CLIConnectionError{Msg: fmt.Sprintf("failed to create stdin pipe: %v", err)}
	}

	t.stdout, err = cmd.StdoutPipe()
	if err != nil {
		return &CLIConnectionError{Msg: fmt.Sprintf("failed to create stdout pipe: %v", err)}
	}

	shouldPipeStderr := t.options.StderrCallback != nil
	if t.options.ExtraArgs != nil {
		if _, ok := t.options.ExtraArgs["debug-to-stderr"]; ok {
			shouldPipeStderr = true
		}
	}
	if shouldPipeStderr {
		t.stderr, err = cmd.StderrPipe()
		if err != nil {
			return &CLIConnectionError{Msg: fmt.Sprintf("failed to create stderr pipe: %v", err)}
		}
	}

	if err := cmd.Start(); err != nil {
		if t.cwd != nil {
			if _, statErr := os.Stat(*t.cwd); statErr != nil {
				connErr := &CLIConnectionError{Msg: fmt.Sprintf("working directory does not exist: %s", *t.cwd)}
				t.exitError = connErr
				return connErr
			}
		}
		connErr := &CLIConnectionError{Msg: fmt.Sprintf("failed to start Claude Code: %v", err)}
		t.exitError = connErr
		return connErr
	}
	t.process = cmd

	if shouldPipeStderr && t.stderr != nil {
		go t.handleStderr()
	}

	t.ready = true
	return nil
}

func (t *SubprocessCLITransport) handleStderr() {
	scanner := bufio.NewScanner(t.stderr)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r\n")
		if line == "" {
			continue
		}
		if t.options.StderrCallback != nil {
			t.options.StderrCallback(line)
		}
	}
}

func (t *SubprocessCLITransport) Write(_ context.Context, data string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.ready || t.stdin == nil {
		return &CLIConnectionError{Msg: "transport is not ready for writing"}
	}

	if t.process != nil && t.process.ProcessState != nil && t.process.ProcessState.Exited() {
		code := t.process.ProcessState.ExitCode()
		return &CLIConnectionError{Msg: fmt.Sprintf("cannot write to terminated process (exit code: %d)", code)}
	}

	if t.exitError != nil {
		return &CLIConnectionError{Msg: fmt.Sprintf("cannot write to process that exited with error: %v", t.exitError)}
	}

	if _, err := io.WriteString(t.stdin, data); err != nil {
		t.ready = false
		t.exitError = &CLIConnectionError{Msg: fmt.Sprintf("failed to write to process stdin: %v", err)}
		return t.exitError
	}
	return nil
}

func (t *SubprocessCLITransport) ReadMessages(ctx context.Context) (<-chan map[string]interface{}, <-chan error) {
	msgCh := make(chan map[string]interface{}, 100)
	errCh := make(chan error, 1)

	go func() {
		defer close(msgCh)
		defer close(errCh)

		if t.stdout == nil {
			errCh <- &CLIConnectionError{Msg: "not connected"}
			return
		}

		scanner := bufio.NewScanner(t.stdout)
		scanner.Buffer(make([]byte, 0, t.maxBufferSize), t.maxBufferSize)

		jsonBuffer := ""

		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
			}

			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}

			jsonLines := strings.Split(line, "\n")
			for _, jl := range jsonLines {
				jl = strings.TrimSpace(jl)
				if jl == "" {
					continue
				}

				jsonBuffer += jl

				if len(jsonBuffer) > t.maxBufferSize {
					errCh <- &CLIJSONDecodeError{
						Line:     fmt.Sprintf("buffer size %d exceeds limit %d", len(jsonBuffer), t.maxBufferSize),
						Original: fmt.Errorf("JSON message exceeded maximum buffer size of %d bytes", t.maxBufferSize),
					}
					jsonBuffer = ""
					continue
				}

				var data map[string]interface{}
				if err := json.Unmarshal([]byte(jsonBuffer), &data); err != nil {
					continue
				}
				jsonBuffer = ""

				select {
				case msgCh <- data:
				case <-ctx.Done():
					return
				}
			}
		}

		if err := scanner.Err(); err != nil {
			select {
			case errCh <- err:
			default:
			}
		}

		if t.process != nil {
			if err := t.process.Wait(); err != nil {
				if exitErr, ok := err.(*exec.ExitError); ok {
					code := exitErr.ExitCode()
					stderrStr := "Check stderr output for details"
					t.exitError = &ProcessError{
						Msg:      fmt.Sprintf("Command failed with exit code %d", code),
						ExitCode: &code,
						Stderr:   &stderrStr,
					}
					select {
					case errCh <- t.exitError:
					default:
					}
				}
			}
		}
	}()

	t.msgCh = msgCh
	t.errCh = errCh
	return msgCh, errCh
}

func (t *SubprocessCLITransport) EndInput() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.stdin != nil {
		err := t.stdin.Close()
		t.stdin = nil
		return err
	}
	return nil
}

func (t *SubprocessCLITransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.ready = false

	if t.stdin != nil {
		t.stdin.Close()
		t.stdin = nil
	}
	if t.stderr != nil {
		t.stderr.Close()
		t.stderr = nil
	}

	if t.process != nil && t.process.Process != nil {
		_ = t.process.Process.Signal(os.Interrupt)
		doneCh := make(chan struct{})
		go func() {
			_ = t.process.Wait()
			close(doneCh)
		}()
		select {
		case <-doneCh:
		case <-time.After(5 * time.Second):
			_ = t.process.Process.Kill()
		}
	}

	t.process = nil
	t.stdout = nil
	t.exitError = nil
	return nil
}

func (t *SubprocessCLITransport) IsReady() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.ready
}

func (t *SubprocessCLITransport) checkClaudeVersion(ctx context.Context) {
	checkCtx, cancel := context.WithTimeout(ctx, versionCheckTimeoutSeconds*time.Second)
	defer cancel()

	cmd := exec.CommandContext(checkCtx, t.cliPath, "-v")
	output, err := cmd.Output()
	if err != nil {
		return
	}

	versionStr := strings.TrimSpace(string(output))
	re := regexp.MustCompile(`^(\d+\.\d+\.\d+)`)
	match := re.FindString(versionStr)
	if match == "" {
		return
	}

	if compareVersions(match, minimumClaudeCodeVersion) < 0 {
		log.Printf("Warning: Claude Code version %s is unsupported in the Agent SDK. "+
			"Minimum required version is %s. Some features may not work correctly.",
			match, minimumClaudeCodeVersion)
	}
}

func compareVersions(a, b string) int {
	pa := strings.Split(a, ".")
	pb := strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		va, _ := strconv.Atoi(pa[i])
		vb, _ := strconv.Atoi(pb[i])
		if va < vb {
			return -1
		}
		if va > vb {
			return 1
		}
	}
	return 0
}
