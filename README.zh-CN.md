# Claude Agent SDK for Go

Go 语言版 Claude Agent SDK。本仓库由 [anthropics/claude-agent-sdk-python](https://github.com/anthropics/claude-agent-sdk-python) 翻译而来。更多信息请参阅 [Claude Agent SDK 文档](https://platform.claude.com/docs/en/agent-sdk/python)。

[English](README.md)

## 安装

```bash
go get github.com/morefun2602/claude-agent-sdk-go
```

**环境要求：** Go 1.25.1+，Claude Code CLI（`npm install -g @anthropic-ai/claude-code`，最低版本 2.0.0）

## 快速开始

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

## 文档

详细用法（包括 **Query**、**Client**、**SDK MCP Server**、**Hooks**、**ClaudeAgentOptions**、消息类型、错误处理和自定义 Transport）请参阅 [claude-agent-sdk-go-usage Skill](skills/claude-agent-sdk-go-usage/SKILL.md)。

## 可用工具

完整工具列表请参阅 [Claude Code 文档](https://docs.anthropic.com/en/docs/claude-code/settings#tools-available-to-claude)。

## 许可证与条款

本 SDK 的使用受 Anthropic [商业服务条款](https://www.anthropic.com/legal/commercial-terms) 约束，包括当你将其用于向自己的客户和最终用户提供产品或服务时，除非特定组件或依赖项在其 LICENSE 文件中另有说明。
