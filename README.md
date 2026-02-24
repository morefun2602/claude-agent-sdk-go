# Claude Agent SDK for Go

Go SDK for Claude Agent. This repository is a Go port translated from [anthropics/claude-agent-sdk-python](https://github.com/anthropics/claude-agent-sdk-python). See the [Claude Agent SDK documentation](https://platform.claude.com/docs/en/agent-sdk/python) for more information.

[中文文档](README.zh-CN.md)

## Installation

```bash
go get github.com/morefun2602/claude-agent-sdk-go
```

**Prerequisites:** Go 1.25.1+, Claude Code CLI (`npm install -g @anthropic-ai/claude-code`, minimum version 2.0.0)

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log"

    claude_agent_sdk "github.com/morefun2602/claude-agent-sdk-go"
)

func main() {
    ctx := context.Background()
    msgCh, errCh := claude_agent_sdk.Query(ctx, claude_agent_sdk.QueryInput{
        Prompt:  "What is 2 + 2?",
        Options: &claude_agent_sdk.ClaudeAgentOptions{},
    })

    for msg := range msgCh {
        if am, ok := msg.(*claude_agent_sdk.AssistantMessage); ok {
            for _, block := range am.Content {
                if tb, ok := block.(claude_agent_sdk.TextBlock); ok {
                    fmt.Print(tb.Text)
                }
            }
        }
    }

    if err := <-errCh; err != nil {
        log.Fatal(err)
    }
}
```

## Documentation

For detailed usage including **Query**, **Client**, **SDK MCP Server**, **Hooks**, **ClaudeAgentOptions**, message types, error handling, and custom Transport, see the [claude-agent-sdk-go-usage Skill](skills/claude-agent-sdk-go-usage/SKILL.md).

## Available Tools

See the [Claude Code documentation](https://docs.anthropic.com/en/docs/claude-code/settings#tools-available-to-claude) for a complete list of available tools.

## License and Terms

Use of this SDK is governed by Anthropic's [Commercial Terms of Service](https://www.anthropic.com/legal/commercial-terms), including when you use it to power products and services that you make available to your own customers and end users, except to the extent a specific component or dependency is covered by a different license as indicated in that component's LICENSE file.
