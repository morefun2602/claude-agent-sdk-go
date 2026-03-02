package claude_agent_sdk

import (
	"context"
	"encoding/json"
)

// PermissionMode controls how tool permissions are handled.
type PermissionMode string

const (
	PermissionModeDefault           PermissionMode = "default"
	PermissionModeAcceptEdits       PermissionMode = "acceptEdits"
	PermissionModePlan              PermissionMode = "plan"
	PermissionModeBypassPermissions PermissionMode = "bypassPermissions"
)

// SdkBeta represents beta feature flags.
type SdkBeta string

const (
	SdkBetaContext1M SdkBeta = "context-1m-2025-08-07"
)

// SettingSource indicates where settings are loaded from.
type SettingSource string

const (
	SettingSourceUser    SettingSource = "user"
	SettingSourceProject SettingSource = "project"
	SettingSourceLocal   SettingSource = "local"
)

// EffortLevel controls thinking depth.
type EffortLevel string

const (
	EffortLow    EffortLevel = "low"
	EffortMedium EffortLevel = "medium"
	EffortHigh   EffortLevel = "high"
	EffortMax    EffortLevel = "max"
)

// --- Content Blocks ---

// ContentBlock is the interface for all content block types.
type ContentBlock interface {
	isContentBlock()
}

type TextBlock struct {
	Text string `json:"text"`
}

func (TextBlock) isContentBlock() {}

type ThinkingBlock struct {
	Thinking  string `json:"thinking"`
	Signature string `json:"signature"`
}

func (ThinkingBlock) isContentBlock() {}

type ToolUseBlock struct {
	ID    string                 `json:"id"`
	Name  string                 `json:"name"`
	Input map[string]interface{} `json:"input"`
}

func (ToolUseBlock) isContentBlock() {}

type ToolResultBlock struct {
	ToolUseID string      `json:"tool_use_id"`
	Content   interface{} `json:"content,omitempty"`
	IsError   *bool       `json:"is_error,omitempty"`
}

func (ToolResultBlock) isContentBlock() {}

// --- Message Types ---

// Message is the interface for all message types returned by the SDK.
type Message interface {
	isMessage()
}

type UserMessage struct {
	Content         interface{}            `json:"content"`
	UUID            *string                `json:"uuid,omitempty"`
	ParentToolUseID *string                `json:"parent_tool_use_id,omitempty"`
	ToolUseResult   map[string]interface{} `json:"tool_use_result,omitempty"`
}

func (UserMessage) isMessage() {}

// AssistantMessageError represents the type of error in an assistant message.
type AssistantMessageError string

const (
	AssistantErrorAuthFailed    AssistantMessageError = "authentication_failed"
	AssistantErrorBilling       AssistantMessageError = "billing_error"
	AssistantErrorRateLimit     AssistantMessageError = "rate_limit"
	AssistantErrorInvalidReq    AssistantMessageError = "invalid_request"
	AssistantErrorServer        AssistantMessageError = "server_error"
	AssistantErrorUnknown       AssistantMessageError = "unknown"
)

type AssistantMessage struct {
	Content         []ContentBlock         `json:"content"`
	Model           string                 `json:"model"`
	ParentToolUseID *string                `json:"parent_tool_use_id,omitempty"`
	Error           *AssistantMessageError `json:"error,omitempty"`
}

func (AssistantMessage) isMessage() {}

type SystemMessage struct {
	Subtype string                 `json:"subtype"`
	Data    map[string]interface{} `json:"data"`
}

func (SystemMessage) isMessage() {}

type ResultMessage struct {
	Subtype          string                 `json:"subtype"`
	DurationMs       int                    `json:"duration_ms"`
	DurationApiMs    int                    `json:"duration_api_ms"`
	IsError          bool                   `json:"is_error"`
	NumTurns         int                    `json:"num_turns"`
	SessionID        string                 `json:"session_id"`
	TotalCostUsd     *float64               `json:"total_cost_usd,omitempty"`
	Usage            map[string]interface{} `json:"usage,omitempty"`
	Result           *string                `json:"result,omitempty"`
	StructuredOutput interface{}            `json:"structured_output,omitempty"`
}

func (ResultMessage) isMessage() {}

type StreamEvent struct {
	UUID            string                 `json:"uuid"`
	SessionID       string                 `json:"session_id"`
	Event           map[string]interface{} `json:"event"`
	ParentToolUseID *string                `json:"parent_tool_use_id,omitempty"`
}

func (StreamEvent) isMessage() {}

// --- System Prompt ---

type SystemPromptPreset struct {
	Type   string `json:"type"`
	Preset string `json:"preset"`
	Append string `json:"append,omitempty"`
}

// SystemPrompt can be a plain string or a SystemPromptPreset.
type SystemPrompt struct {
	Text   *string
	Preset *SystemPromptPreset
}

// --- Tools ---

type ToolsPreset struct {
	Type   string `json:"type"`
	Preset string `json:"preset"`
}

// Tools can be a list of tool names or a ToolsPreset.
type Tools struct {
	Names  []string
	Preset *ToolsPreset
}

// --- Agent Definition ---

type AgentDefinition struct {
	Description string  `json:"description"`
	Prompt      string  `json:"prompt"`
	Tools       []string `json:"tools,omitempty"`
	Model       *string `json:"model,omitempty"`
}

// --- Permission Types ---

type PermissionBehavior string

const (
	PermissionBehaviorAllow PermissionBehavior = "allow"
	PermissionBehaviorDeny  PermissionBehavior = "deny"
	PermissionBehaviorAsk   PermissionBehavior = "ask"
)

type PermissionUpdateDestination string

const (
	PermDestUserSettings    PermissionUpdateDestination = "userSettings"
	PermDestProjectSettings PermissionUpdateDestination = "projectSettings"
	PermDestLocalSettings   PermissionUpdateDestination = "localSettings"
	PermDestSession         PermissionUpdateDestination = "session"
)

type PermissionRuleValue struct {
	ToolName    string  `json:"toolName"`
	RuleContent *string `json:"ruleContent,omitempty"`
}

type PermissionUpdate struct {
	Type        string                       `json:"type"`
	Rules       []PermissionRuleValue        `json:"rules,omitempty"`
	Behavior    *PermissionBehavior          `json:"behavior,omitempty"`
	Mode        *PermissionMode              `json:"mode,omitempty"`
	Directories []string                     `json:"directories,omitempty"`
	Destination *PermissionUpdateDestination `json:"destination,omitempty"`
}

type ToolPermissionContext struct {
	Suggestions []PermissionUpdate `json:"suggestions"`
}

type PermissionResultAllow struct {
	Behavior           string             `json:"behavior"`
	UpdatedInput       map[string]interface{} `json:"updatedInput,omitempty"`
	UpdatedPermissions []PermissionUpdate `json:"updatedPermissions,omitempty"`
}

type PermissionResultDeny struct {
	Behavior  string `json:"behavior"`
	Message   string `json:"message,omitempty"`
	Interrupt bool   `json:"interrupt,omitempty"`
}

// PermissionResult is either PermissionResultAllow or PermissionResultDeny.
type PermissionResult interface {
	isPermissionResult()
}

func (PermissionResultAllow) isPermissionResult() {}
func (PermissionResultDeny) isPermissionResult()  {}

// CanUseToolFunc is the callback type for tool permission requests.
type CanUseToolFunc func(ctx context.Context, toolName string, input map[string]interface{}, permCtx ToolPermissionContext) (PermissionResult, error)

// --- Hook Types ---

// HookEvent represents the type of hook event.
type HookEvent string

const (
	HookEventPreToolUse          HookEvent = "PreToolUse"
	HookEventPostToolUse         HookEvent = "PostToolUse"
	HookEventPostToolUseFailure  HookEvent = "PostToolUseFailure"
	HookEventUserPromptSubmit    HookEvent = "UserPromptSubmit"
	HookEventStop                HookEvent = "Stop"
	HookEventSubagentStop        HookEvent = "SubagentStop"
	HookEventPreCompact          HookEvent = "PreCompact"
	HookEventNotification        HookEvent = "Notification"
	HookEventSubagentStart       HookEvent = "SubagentStart"
	HookEventPermissionRequest   HookEvent = "PermissionRequest"
)

// HookCallbackFunc is the callback signature for hook events.
// Parameters: input data, tool_use_id (may be nil), context.
// Returns: output map for the CLI.
type HookCallbackFunc func(ctx context.Context, input map[string]interface{}, toolUseID *string) (map[string]interface{}, error)

// HookMatcher matches tool names and routes to callbacks.
type HookMatcher struct {
	Matcher  *string            `json:"matcher,omitempty"`
	Hooks    []HookCallbackFunc `json:"-"`
	Timeout  *float64           `json:"timeout,omitempty"`
}

// --- MCP Server Configuration ---

// McpServerConfig is the interface for all MCP server configuration types.
type McpServerConfig interface {
	mcpServerConfigType() string
}

type McpStdioServerConfig struct {
	Type    string            `json:"type,omitempty"`
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

func (McpStdioServerConfig) mcpServerConfigType() string { return "stdio" }

type McpSSEServerConfig struct {
	Type    string            `json:"type"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

func (McpSSEServerConfig) mcpServerConfigType() string { return "sse" }

type McpHttpServerConfig struct {
	Type    string            `json:"type"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

func (McpHttpServerConfig) mcpServerConfigType() string { return "http" }

type McpSdkServerConfig struct {
	Type     string            `json:"type"`
	Name     string            `json:"name"`
	Instance *McpServerInstance `json:"-"`
}

func (McpSdkServerConfig) mcpServerConfigType() string { return "sdk" }

// McpServerInstance represents an in-process MCP server for SDK tools.
type McpServerInstance struct {
	Name    string
	Version string
	Tools   []SdkMcpTool
	toolMap map[string]*SdkMcpTool
}

// SdkMcpTool defines a single tool in an SDK MCP server.
type SdkMcpTool struct {
	Name        string
	Description string
	InputSchema interface{}
	Handler     func(ctx context.Context, args map[string]interface{}) (map[string]interface{}, error)
}

// --- Sandbox Configuration ---

type SandboxNetworkConfig struct {
	AllowUnixSockets    []string `json:"allowUnixSockets,omitempty"`
	AllowAllUnixSockets *bool    `json:"allowAllUnixSockets,omitempty"`
	AllowLocalBinding   *bool    `json:"allowLocalBinding,omitempty"`
	HttpProxyPort       *int     `json:"httpProxyPort,omitempty"`
	SocksProxyPort      *int     `json:"socksProxyPort,omitempty"`
}

type SandboxIgnoreViolations struct {
	File    []string `json:"file,omitempty"`
	Network []string `json:"network,omitempty"`
}

type SandboxSettings struct {
	Enabled                    *bool                    `json:"enabled,omitempty"`
	AutoAllowBashIfSandboxed   *bool                    `json:"autoAllowBashIfSandboxed,omitempty"`
	ExcludedCommands           []string                 `json:"excludedCommands,omitempty"`
	AllowUnsandboxedCommands   *bool                    `json:"allowUnsandboxedCommands,omitempty"`
	Network                    *SandboxNetworkConfig    `json:"network,omitempty"`
	IgnoreViolations           *SandboxIgnoreViolations `json:"ignoreViolations,omitempty"`
	EnableWeakerNestedSandbox  *bool                    `json:"enableWeakerNestedSandbox,omitempty"`
}

// --- Plugin Configuration ---

type SdkPluginConfig struct {
	Type string `json:"type"`
	Path string `json:"path"`
}

// --- Thinking Configuration ---

type ThinkingConfigType string

const (
	ThinkingAdaptive ThinkingConfigType = "adaptive"
	ThinkingEnabled  ThinkingConfigType = "enabled"
	ThinkingDisabled ThinkingConfigType = "disabled"
)

type ThinkingConfig struct {
	Type         ThinkingConfigType `json:"type"`
	BudgetTokens *int              `json:"budget_tokens,omitempty"`
}

// --- Main Options ---

// ClaudeAgentOptions holds all configuration for a Claude Agent SDK session.
type ClaudeAgentOptions struct {
	Tools                    *Tools                       `json:"-"`
	AllowedTools             []string                     `json:"allowed_tools,omitempty"`
	SystemPrompt             *SystemPrompt                `json:"-"`
	McpServers               map[string]McpServerConfig   `json:"-"`
	McpServersPath           *string                      `json:"-"`
	PermissionMode           *PermissionMode              `json:"permission_mode,omitempty"`
	ContinueConversation     bool                         `json:"continue_conversation,omitempty"`
	Resume                   *string                      `json:"resume,omitempty"`
	SessionID                *string                      `json:"session_id,omitempty"`
	MaxTurns                 *int                         `json:"max_turns,omitempty"`
	MaxBudgetUsd             *float64                     `json:"max_budget_usd,omitempty"`
	DisallowedTools          []string                     `json:"disallowed_tools,omitempty"`
	Model                    *string                      `json:"model,omitempty"`
	FallbackModel            *string                      `json:"fallback_model,omitempty"`
	Betas                    []SdkBeta                    `json:"betas,omitempty"`
	PermissionPromptToolName *string                      `json:"permission_prompt_tool_name,omitempty"`
	Cwd                      *string                      `json:"cwd,omitempty"`
	CLIPath                  *string                      `json:"-"`
	Settings                 *string                      `json:"-"`
	AddDirs                  []string                     `json:"add_dirs,omitempty"`
	Env                      map[string]string            `json:"-"`
	ExtraArgs                map[string]*string           `json:"-"`
	MaxBufferSize            *int                         `json:"-"`
	StderrCallback           func(string)                 `json:"-"`
	CanUseTool               CanUseToolFunc               `json:"-"`
	Hooks                    map[HookEvent][]HookMatcher  `json:"-"`
	User                     *string                      `json:"-"`
	IncludePartialMessages   bool                         `json:"-"`
	ForkSession              bool                         `json:"-"`
	Agents                   map[string]AgentDefinition   `json:"-"`
	SettingSources           []SettingSource              `json:"-"`
	Sandbox                  *SandboxSettings             `json:"-"`
	Plugins                  []SdkPluginConfig            `json:"-"`
	MaxThinkingTokens        *int                         `json:"-"`
	Thinking                 *ThinkingConfig              `json:"-"`
	Effort                   *EffortLevel                 `json:"-"`
	OutputFormat             map[string]interface{}       `json:"-"`
	EnableFileCheckpointing  bool                         `json:"-"`
}

// --- Control Protocol Types ---

type controlRequest struct {
	Type      string          `json:"type"`
	RequestID string          `json:"request_id"`
	Request   json.RawMessage `json:"request"`
}

type controlRequestPayload struct {
	Subtype string `json:"subtype"`
}

type controlPermissionRequest struct {
	Subtype               string                   `json:"subtype"`
	ToolName              string                   `json:"tool_name"`
	Input                 map[string]interface{}    `json:"input"`
	PermissionSuggestions []map[string]interface{}  `json:"permission_suggestions,omitempty"`
	BlockedPath           *string                  `json:"blocked_path,omitempty"`
}

type controlHookCallbackRequest struct {
	Subtype    string      `json:"subtype"`
	CallbackID string      `json:"callback_id"`
	Input      interface{} `json:"input"`
	ToolUseID  *string     `json:"tool_use_id,omitempty"`
}

type controlMcpMessageRequest struct {
	Subtype    string      `json:"subtype"`
	ServerName string      `json:"server_name"`
	Message    interface{} `json:"message"`
}

type controlResponseMsg struct {
	Type     string          `json:"type"`
	Response json.RawMessage `json:"response"`
}

type controlResponsePayload struct {
	Subtype   string                 `json:"subtype"`
	RequestID string                 `json:"request_id"`
	Response  map[string]interface{} `json:"response,omitempty"`
	Error     string                 `json:"error,omitempty"`
}

// QueryInput holds parameters for the one-shot Query function.
type QueryInput struct {
	Prompt    string
	Stream    <-chan map[string]interface{}
	Options   *ClaudeAgentOptions
	Transport Transport
}
