package claude_agent_sdk

import "fmt"

// ParseMessage converts a raw JSON map from CLI output into a typed Message.
func ParseMessage(data map[string]interface{}) (Message, error) {
	if data == nil {
		return nil, &MessageParseError{Msg: "invalid message data: nil"}
	}

	msgType, _ := data["type"].(string)
	if msgType == "" {
		return nil, &MessageParseError{Msg: "message missing 'type' field", Data: data}
	}

	switch msgType {
	case "user":
		return parseUserMessage(data)
	case "assistant":
		return parseAssistantMessage(data)
	case "system":
		return parseSystemMessage(data)
	case "result":
		return parseResultMessage(data)
	case "stream_event":
		return parseStreamEvent(data)
	default:
		return nil, &MessageParseError{Msg: fmt.Sprintf("unknown message type: %s", msgType), Data: data}
	}
}

func parseUserMessage(data map[string]interface{}) (*UserMessage, error) {
	msg := &UserMessage{}

	msg.UUID = getStringPtr(data, "uuid")
	msg.ParentToolUseID = getStringPtr(data, "parent_tool_use_id")

	if tur, ok := data["tool_use_result"]; ok {
		if m, ok := tur.(map[string]interface{}); ok {
			msg.ToolUseResult = m
		}
	}

	messageObj, ok := data["message"].(map[string]interface{})
	if !ok {
		return nil, &MessageParseError{Msg: "missing 'message' field in user message", Data: data}
	}

	content := messageObj["content"]
	switch c := content.(type) {
	case string:
		msg.Content = c
	case []interface{}:
		blocks, err := parseContentBlocks(c)
		if err != nil {
			return nil, &MessageParseError{Msg: fmt.Sprintf("error parsing user content blocks: %v", err), Data: data}
		}
		msg.Content = blocks
	default:
		msg.Content = content
	}

	return msg, nil
}

func parseAssistantMessage(data map[string]interface{}) (*AssistantMessage, error) {
	msg := &AssistantMessage{}
	msg.ParentToolUseID = getStringPtr(data, "parent_tool_use_id")

	if errVal, ok := data["error"].(string); ok {
		e := AssistantMessageError(errVal)
		msg.Error = &e
	}

	messageObj, ok := data["message"].(map[string]interface{})
	if !ok {
		return nil, &MessageParseError{Msg: "missing 'message' field in assistant message", Data: data}
	}

	model, _ := messageObj["model"].(string)
	msg.Model = model

	contentArr, ok := messageObj["content"].([]interface{})
	if !ok {
		return nil, &MessageParseError{Msg: "missing 'content' in assistant message", Data: data}
	}

	blocks, err := parseContentBlocks(contentArr)
	if err != nil {
		return nil, &MessageParseError{Msg: fmt.Sprintf("error parsing assistant content blocks: %v", err), Data: data}
	}
	msg.Content = blocks

	return msg, nil
}

func parseSystemMessage(data map[string]interface{}) (*SystemMessage, error) {
	subtype, ok := data["subtype"].(string)
	if !ok {
		return nil, &MessageParseError{Msg: "missing 'subtype' in system message", Data: data}
	}
	return &SystemMessage{
		Subtype: subtype,
		Data:    data,
	}, nil
}

func parseResultMessage(data map[string]interface{}) (*ResultMessage, error) {
	msg := &ResultMessage{}

	var ok bool
	msg.Subtype, ok = data["subtype"].(string)
	if !ok {
		return nil, &MessageParseError{Msg: "missing 'subtype' in result message", Data: data}
	}
	msg.DurationMs = getInt(data, "duration_ms")
	msg.DurationApiMs = getInt(data, "duration_api_ms")
	msg.IsError, _ = data["is_error"].(bool)
	msg.NumTurns = getInt(data, "num_turns")

	msg.SessionID, ok = data["session_id"].(string)
	if !ok {
		return nil, &MessageParseError{Msg: "missing 'session_id' in result message", Data: data}
	}

	if v, ok := data["total_cost_usd"].(float64); ok {
		msg.TotalCostUsd = &v
	}
	if v, ok := data["usage"].(map[string]interface{}); ok {
		msg.Usage = v
	}
	msg.Result = getStringPtr(data, "result")
	msg.StructuredOutput = data["structured_output"]

	return msg, nil
}

func parseStreamEvent(data map[string]interface{}) (*StreamEvent, error) {
	uuid, ok := data["uuid"].(string)
	if !ok {
		return nil, &MessageParseError{Msg: "missing 'uuid' in stream_event", Data: data}
	}
	sessionID, ok := data["session_id"].(string)
	if !ok {
		return nil, &MessageParseError{Msg: "missing 'session_id' in stream_event", Data: data}
	}
	event, ok := data["event"].(map[string]interface{})
	if !ok {
		return nil, &MessageParseError{Msg: "missing 'event' in stream_event", Data: data}
	}

	return &StreamEvent{
		UUID:            uuid,
		SessionID:       sessionID,
		Event:           event,
		ParentToolUseID: getStringPtr(data, "parent_tool_use_id"),
	}, nil
}

func parseContentBlocks(arr []interface{}) ([]ContentBlock, error) {
	blocks := make([]ContentBlock, 0, len(arr))
	for _, item := range arr {
		blockMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		blockType, _ := blockMap["type"].(string)
		switch blockType {
		case "text":
			text, _ := blockMap["text"].(string)
			blocks = append(blocks, TextBlock{Text: text})
		case "thinking":
			thinking, _ := blockMap["thinking"].(string)
			sig, _ := blockMap["signature"].(string)
			blocks = append(blocks, ThinkingBlock{Thinking: thinking, Signature: sig})
		case "tool_use":
			id, _ := blockMap["id"].(string)
			name, _ := blockMap["name"].(string)
			input, _ := blockMap["input"].(map[string]interface{})
			blocks = append(blocks, ToolUseBlock{ID: id, Name: name, Input: input})
		case "tool_result":
			toolUseID, _ := blockMap["tool_use_id"].(string)
			block := ToolResultBlock{ToolUseID: toolUseID, Content: blockMap["content"]}
			if isErr, ok := blockMap["is_error"].(bool); ok {
				block.IsError = &isErr
			}
			blocks = append(blocks, block)
		}
	}
	return blocks, nil
}

// --- Helpers ---

func getStringPtr(m map[string]interface{}, key string) *string {
	if v, ok := m[key].(string); ok {
		return &v
	}
	return nil
}

func getInt(m map[string]interface{}, key string) int {
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}
