package claude_agent_sdk

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"time"
)

type controlResult struct {
	response map[string]interface{}
	err      error
}

type hookMatcherInternal struct {
	Matcher        *string
	HookCallbackIDs []string
	Timeout        *float64
}

// queryHandler implements the bidirectional control protocol on top of Transport.
type queryHandler struct {
	transport     Transport
	canUseTool    CanUseToolFunc
	hooks         map[string][]hookMatcherInternal
	sdkMcpServers map[string]*McpServerInstance
	agents        map[string]map[string]interface{}

	hookCallbacks    map[string]HookCallbackFunc
	nextCallbackID   int
	requestCounter   int
	pendingResponses map[string]chan controlResult

	messageCh chan map[string]interface{}

	initializeTimeout float64
	initialized       bool
	initResult        map[string]interface{}

	firstResultCh      chan struct{}
	firstResultOnce    sync.Once
	streamCloseTimeout float64

	closed bool
	mu     sync.Mutex
	wg     sync.WaitGroup

	cancel context.CancelFunc
	ctx    context.Context
}

type queryHandlerConfig struct {
	transport         Transport
	canUseTool        CanUseToolFunc
	hooks             map[HookEvent][]HookMatcher
	sdkMcpServers     map[string]*McpServerInstance
	agents            map[string]map[string]interface{}
	initializeTimeout float64
}

func newQueryHandler(cfg queryHandlerConfig) *queryHandler {
	ctx, cancel := context.WithCancel(context.Background())

	timeout := cfg.initializeTimeout
	if timeout <= 0 {
		timeout = 60.0
	}

	streamCloseTimeout := 60.0
	if v := os.Getenv("CLAUDE_CODE_STREAM_CLOSE_TIMEOUT"); v != "" {
		if ms, err := strconv.ParseFloat(v, 64); err == nil {
			streamCloseTimeout = ms / 1000.0
		}
	}

	qh := &queryHandler{
		transport:          cfg.transport,
		canUseTool:         cfg.canUseTool,
		sdkMcpServers:      cfg.sdkMcpServers,
		agents:             cfg.agents,
		hookCallbacks:      make(map[string]HookCallbackFunc),
		pendingResponses:   make(map[string]chan controlResult),
		messageCh:          make(chan map[string]interface{}, 100),
		initializeTimeout:  timeout,
		firstResultCh:      make(chan struct{}),
		streamCloseTimeout: streamCloseTimeout,
		ctx:                ctx,
		cancel:             cancel,
	}

	qh.hooks = make(map[string][]hookMatcherInternal)
	if cfg.hooks != nil {
		for event, matchers := range cfg.hooks {
			for _, m := range matchers {
				callbackIDs := make([]string, 0, len(m.Hooks))
				for _, cb := range m.Hooks {
					id := fmt.Sprintf("hook_%d", qh.nextCallbackID)
					qh.nextCallbackID++
					qh.hookCallbacks[id] = cb
					callbackIDs = append(callbackIDs, id)
				}
				qh.hooks[string(event)] = append(qh.hooks[string(event)], hookMatcherInternal{
					Matcher:         m.Matcher,
					HookCallbackIDs: callbackIDs,
					Timeout:         m.Timeout,
				})
			}
		}
	}

	return qh
}

func (qh *queryHandler) start(ctx context.Context) {
	msgCh, errCh := qh.transport.ReadMessages(ctx)

	qh.wg.Add(1)
	go func() {
		defer qh.wg.Done()
		defer close(qh.messageCh)
		qh.readMessages(msgCh, errCh)
	}()
}

func (qh *queryHandler) readMessages(msgCh <-chan map[string]interface{}, errCh <-chan error) {
	for {
		select {
		case <-qh.ctx.Done():
			return
		case err, ok := <-errCh:
			if !ok {
				return
			}
			if err != nil {
				log.Printf("transport error: %v", err)
				qh.failAllPending(err)
				qh.messageCh <- map[string]interface{}{"type": "error", "error": err.Error()}
			}
			return
		case msg, ok := <-msgCh:
			if !ok {
				return
			}
			if qh.closed {
				return
			}

			msgType, _ := msg["type"].(string)

			switch msgType {
			case "control_response":
				qh.handleControlResponse(msg)
			case "control_request":
				go qh.handleControlRequest(msg)
			case "control_cancel_request":
				continue
			default:
				if msgType == "result" {
					qh.firstResultOnce.Do(func() { close(qh.firstResultCh) })
				}
				select {
				case qh.messageCh <- msg:
				case <-qh.ctx.Done():
					return
				}
			}
		}
	}
}

func (qh *queryHandler) handleControlResponse(msg map[string]interface{}) {
	respRaw, ok := msg["response"]
	if !ok {
		return
	}
	respMap, ok := respRaw.(map[string]interface{})
	if !ok {
		return
	}

	requestID, _ := respMap["request_id"].(string)
	if requestID == "" {
		return
	}

	qh.mu.Lock()
	ch, exists := qh.pendingResponses[requestID]
	qh.mu.Unlock()

	if !exists {
		return
	}

	subtype, _ := respMap["subtype"].(string)
	if subtype == "error" {
		errMsg, _ := respMap["error"].(string)
		ch <- controlResult{err: fmt.Errorf("%s", errMsg)}
	} else {
		resp, _ := respMap["response"].(map[string]interface{})
		ch <- controlResult{response: resp}
	}
}

func (qh *queryHandler) handleControlRequest(msg map[string]interface{}) {
	requestID, _ := msg["request_id"].(string)
	reqRaw, ok := msg["request"]
	if !ok {
		return
	}
	reqMap, ok := reqRaw.(map[string]interface{})
	if !ok {
		return
	}

	subtype, _ := reqMap["subtype"].(string)

	var responseData map[string]interface{}
	var handleErr error

	switch subtype {
	case "can_use_tool":
		responseData, handleErr = qh.handleCanUseTool(reqMap)
	case "hook_callback":
		responseData, handleErr = qh.handleHookCallback(reqMap)
	case "mcp_message":
		responseData, handleErr = qh.handleMcpMessage(reqMap)
	default:
		handleErr = fmt.Errorf("unsupported control request subtype: %s", subtype)
	}

	var response map[string]interface{}
	if handleErr != nil {
		response = map[string]interface{}{
			"type": "control_response",
			"response": map[string]interface{}{
				"subtype":    "error",
				"request_id": requestID,
				"error":      handleErr.Error(),
			},
		}
	} else {
		response = map[string]interface{}{
			"type": "control_response",
			"response": map[string]interface{}{
				"subtype":    "success",
				"request_id": requestID,
				"response":   responseData,
			},
		}
	}

	b, _ := json.Marshal(response)
	_ = qh.transport.Write(qh.ctx, string(b)+"\n")
}

func (qh *queryHandler) handleCanUseTool(req map[string]interface{}) (map[string]interface{}, error) {
	if qh.canUseTool == nil {
		return nil, fmt.Errorf("canUseTool callback is not provided")
	}

	toolName, _ := req["tool_name"].(string)
	input, _ := req["input"].(map[string]interface{})
	if input == nil {
		input = map[string]interface{}{}
	}

	var suggestions []PermissionUpdate
	if sugRaw, ok := req["permission_suggestions"].([]interface{}); ok {
		for range sugRaw {
			suggestions = append(suggestions, PermissionUpdate{})
		}
	}

	permCtx := ToolPermissionContext{Suggestions: suggestions}
	result, err := qh.canUseTool(qh.ctx, toolName, input, permCtx)
	if err != nil {
		return nil, err
	}

	switch r := result.(type) {
	case PermissionResultAllow:
		resp := map[string]interface{}{
			"behavior": "allow",
		}
		if r.UpdatedInput != nil {
			resp["updatedInput"] = r.UpdatedInput
		} else {
			resp["updatedInput"] = input
		}
		if r.UpdatedPermissions != nil {
			resp["updatedPermissions"] = r.UpdatedPermissions
		}
		return resp, nil
	case PermissionResultDeny:
		resp := map[string]interface{}{
			"behavior": "deny",
			"message":  r.Message,
		}
		if r.Interrupt {
			resp["interrupt"] = true
		}
		return resp, nil
	default:
		return nil, fmt.Errorf("unexpected permission result type: %T", result)
	}
}

func (qh *queryHandler) handleHookCallback(req map[string]interface{}) (map[string]interface{}, error) {
	callbackID, _ := req["callback_id"].(string)
	callback, ok := qh.hookCallbacks[callbackID]
	if !ok {
		return nil, fmt.Errorf("no hook callback found for ID: %s", callbackID)
	}

	input, _ := req["input"].(map[string]interface{})
	var toolUseID *string
	if v, ok := req["tool_use_id"].(string); ok {
		toolUseID = &v
	}

	output, err := callback(qh.ctx, input, toolUseID)
	if err != nil {
		return nil, err
	}

	return convertHookOutputForCLI(output), nil
}

func convertHookOutputForCLI(output map[string]interface{}) map[string]interface{} {
	converted := make(map[string]interface{}, len(output))
	for k, v := range output {
		switch k {
		case "async_":
			converted["async"] = v
		case "continue_":
			converted["continue"] = v
		default:
			converted[k] = v
		}
	}
	return converted
}

func (qh *queryHandler) handleMcpMessage(req map[string]interface{}) (map[string]interface{}, error) {
	serverName, _ := req["server_name"].(string)
	message, _ := req["message"].(map[string]interface{})
	if serverName == "" || message == nil {
		return nil, fmt.Errorf("missing server_name or message for MCP request")
	}

	mcpResp := qh.handleSdkMcpRequest(serverName, message)
	return map[string]interface{}{"mcp_response": mcpResp}, nil
}

func (qh *queryHandler) handleSdkMcpRequest(serverName string, message map[string]interface{}) map[string]interface{} {
	server, ok := qh.sdkMcpServers[serverName]
	if !ok {
		return map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      message["id"],
			"error": map[string]interface{}{
				"code":    -32601,
				"message": fmt.Sprintf("Server '%s' not found", serverName),
			},
		}
	}

	method, _ := message["method"].(string)
	params, _ := message["params"].(map[string]interface{})

	switch method {
	case "initialize":
		return map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      message["id"],
			"result": map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities":   map[string]interface{}{"tools": map[string]interface{}{}},
				"serverInfo": map[string]interface{}{
					"name":    server.Name,
					"version": server.Version,
				},
			},
		}

	case "tools/list":
		toolsData := make([]map[string]interface{}, 0, len(server.Tools))
		for _, tool := range server.Tools {
			td := map[string]interface{}{
				"name":        tool.Name,
				"description": tool.Description,
			}
			if tool.InputSchema != nil {
				td["inputSchema"] = tool.InputSchema
			} else {
				td["inputSchema"] = map[string]interface{}{}
			}
			toolsData = append(toolsData, td)
		}
		return map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      message["id"],
			"result":  map[string]interface{}{"tools": toolsData},
		}

	case "tools/call":
		name, _ := params["name"].(string)
		args, _ := params["arguments"].(map[string]interface{})
		if args == nil {
			args = map[string]interface{}{}
		}

		if server.toolMap == nil {
			server.toolMap = make(map[string]*SdkMcpTool, len(server.Tools))
			for i := range server.Tools {
				server.toolMap[server.Tools[i].Name] = &server.Tools[i]
			}
		}

		tool, exists := server.toolMap[name]
		if !exists {
			return map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      message["id"],
				"error": map[string]interface{}{
					"code":    -32601,
					"message": fmt.Sprintf("Tool '%s' not found", name),
				},
			}
		}

		result, err := tool.Handler(qh.ctx, args)
		if err != nil {
			return map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      message["id"],
				"error": map[string]interface{}{
					"code":    -32603,
					"message": err.Error(),
				},
			}
		}

		return map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      message["id"],
			"result":  result,
		}

	case "notifications/initialized":
		return map[string]interface{}{"jsonrpc": "2.0", "result": map[string]interface{}{}}

	default:
		return map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      message["id"],
			"error": map[string]interface{}{
				"code":    -32601,
				"message": fmt.Sprintf("Method '%s' not found", method),
			},
		}
	}
}

func (qh *queryHandler) sendControlRequest(ctx context.Context, request map[string]interface{}, timeout time.Duration) (map[string]interface{}, error) {
	qh.mu.Lock()
	qh.requestCounter++
	randBytes := make([]byte, 4)
	_, _ = rand.Read(randBytes)
	requestID := fmt.Sprintf("req_%d_%s", qh.requestCounter, hex.EncodeToString(randBytes))

	ch := make(chan controlResult, 1)
	qh.pendingResponses[requestID] = ch
	qh.mu.Unlock()

	controlReq := map[string]interface{}{
		"type":       "control_request",
		"request_id": requestID,
		"request":    request,
	}

	b, err := json.Marshal(controlReq)
	if err != nil {
		qh.mu.Lock()
		delete(qh.pendingResponses, requestID)
		qh.mu.Unlock()
		return nil, fmt.Errorf("marshal control request: %w", err)
	}

	if err := qh.transport.Write(ctx, string(b)+"\n"); err != nil {
		qh.mu.Lock()
		delete(qh.pendingResponses, requestID)
		qh.mu.Unlock()
		return nil, fmt.Errorf("write control request: %w", err)
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	select {
	case result := <-ch:
		qh.mu.Lock()
		delete(qh.pendingResponses, requestID)
		qh.mu.Unlock()
		if result.err != nil {
			return nil, result.err
		}
		if result.response == nil {
			return map[string]interface{}{}, nil
		}
		return result.response, nil
	case <-timeoutCtx.Done():
		qh.mu.Lock()
		delete(qh.pendingResponses, requestID)
		qh.mu.Unlock()
		subtype, _ := request["subtype"].(string)
		return nil, fmt.Errorf("control request timeout: %s", subtype)
	}
}

func (qh *queryHandler) initialize(ctx context.Context) (map[string]interface{}, error) {
	hooksConfig := map[string]interface{}{}
	for event, matchers := range qh.hooks {
		if len(matchers) > 0 {
			matcherConfigs := make([]map[string]interface{}, 0, len(matchers))
			for _, m := range matchers {
				mc := map[string]interface{}{
					"matcher":         m.Matcher,
					"hookCallbackIds": m.HookCallbackIDs,
				}
				if m.Timeout != nil {
					mc["timeout"] = *m.Timeout
				}
				matcherConfigs = append(matcherConfigs, mc)
			}
			hooksConfig[event] = matcherConfigs
		}
	}

	request := map[string]interface{}{
		"subtype": "initialize",
	}
	if len(hooksConfig) > 0 {
		request["hooks"] = hooksConfig
	} else {
		request["hooks"] = nil
	}
	if qh.agents != nil {
		request["agents"] = qh.agents
	}

	timeout := time.Duration(qh.initializeTimeout * float64(time.Second))
	resp, err := qh.sendControlRequest(ctx, request, timeout)
	if err != nil {
		return nil, err
	}
	qh.initialized = true
	qh.initResult = resp
	return resp, nil
}

func (qh *queryHandler) interrupt(ctx context.Context) error {
	_, err := qh.sendControlRequest(ctx, map[string]interface{}{"subtype": "interrupt"}, 60*time.Second)
	return err
}

func (qh *queryHandler) setPermissionMode(ctx context.Context, mode string) error {
	_, err := qh.sendControlRequest(ctx, map[string]interface{}{
		"subtype": "set_permission_mode",
		"mode":    mode,
	}, 60*time.Second)
	return err
}

func (qh *queryHandler) setModel(ctx context.Context, model *string) error {
	_, err := qh.sendControlRequest(ctx, map[string]interface{}{
		"subtype": "set_model",
		"model":   model,
	}, 60*time.Second)
	return err
}

func (qh *queryHandler) rewindFiles(ctx context.Context, userMessageID string) error {
	_, err := qh.sendControlRequest(ctx, map[string]interface{}{
		"subtype":         "rewind_files",
		"user_message_id": userMessageID,
	}, 60*time.Second)
	return err
}

func (qh *queryHandler) getMcpStatus(ctx context.Context) (map[string]interface{}, error) {
	return qh.sendControlRequest(ctx, map[string]interface{}{"subtype": "mcp_status"}, 60*time.Second)
}

// streamInput writes messages from a channel to the transport.
// If SDK MCP servers or hooks are present, waits for the first result before closing stdin.
func (qh *queryHandler) streamInput(ctx context.Context, stream <-chan map[string]interface{}) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-stream:
			if !ok {
				goto done
			}
			if qh.closed {
				return
			}
			b, err := json.Marshal(msg)
			if err != nil {
				continue
			}
			_ = qh.transport.Write(ctx, string(b)+"\n")
		}
	}

done:
	hasHooks := len(qh.hooks) > 0
	if len(qh.sdkMcpServers) > 0 || hasHooks {
		select {
		case <-qh.firstResultCh:
		case <-time.After(time.Duration(qh.streamCloseTimeout * float64(time.Second))):
		case <-ctx.Done():
		}
	}

	_ = qh.transport.EndInput()
}

// receiveMessages returns the message channel for consuming parsed messages.
func (qh *queryHandler) receiveMessages() <-chan map[string]interface{} {
	return qh.messageCh
}

func (qh *queryHandler) close() {
	qh.closed = true
	qh.cancel()
	qh.wg.Wait()
	_ = qh.transport.Close()
}

func (qh *queryHandler) failAllPending(err error) {
	qh.mu.Lock()
	defer qh.mu.Unlock()
	for id, ch := range qh.pendingResponses {
		select {
		case ch <- controlResult{err: err}:
		default:
		}
		delete(qh.pendingResponses, id)
	}
}
