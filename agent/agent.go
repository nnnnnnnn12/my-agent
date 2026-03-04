package agent

import (
	"encoding/json"
	"fmt"
	"my-agent/llm"
	"my-agent/tools"
)

type ToolAgent struct {
	llmClient *llm.Client
	memory    *Memory
	tools     map[string]tools.Tool
	toolDefs  []llm.Tool
}

func NewToolAgent(systemPrompt string) *ToolAgent {
	return &ToolAgent{
		llmClient: llm.NewClient(),
		memory:    NewMemory(systemPrompt),
		tools:     make(map[string]tools.Tool),
	}
}

func (a *ToolAgent) RegisterTool(t tools.Tool) {
	a.tools[t.Name()] = t
	a.toolDefs = append(a.toolDefs, llm.Tool{
		Type: "function",
		Function: llm.FunctionDetail{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.Parameters(),
		},
	})
	fmt.Printf("✅ 已注册工具: %s\n", t.Name())
}

// Chat 实现了 ReAct 循环：思考 → 行动 → 观察 → 再思考
func (a *ToolAgent) Chat(userInput string) (string, error) {
	a.memory.AddUserMessage(userInput)

	for i := 0; i < 5; i++ {
		result, err := a.llmClient.Chat(a.memory.GetMessages(), a.toolDefs)
		if err != nil {
			return "", fmt.Errorf("LLM调用失败: %w", err)
		}

		// 情况1：LLM直接给出答案
		if !result.IsToolCall {
			a.memory.AddAssistantMessage(result.Content)
			return result.Content, nil
		}

		// 情况2：LLM决定调用工具
		fmt.Printf("🔧 Agent决定调用工具...\n")
		a.memory.AddToolCallIntent(result.ToolCalls)

		for _, toolCall := range result.ToolCalls {
			toolResult := a.executeTool(toolCall)
			a.memory.AddToolResult(toolCall.ID, toolCall.Function.Name, toolResult)
		}

		fmt.Printf("🔄 Agent根据工具结果继续思考...\n")
	}

	return "", fmt.Errorf("超过最大循环次数")
}

func (a *ToolAgent) executeTool(toolCall llm.ToolCall) string {
	tool, exists := a.tools[toolCall.Function.Name]
	if !exists {
		return fmt.Sprintf("错误：工具 '%s' 不存在", toolCall.Function.Name)
	}

	var args map[string]interface{}
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		return fmt.Sprintf("参数解析失败: %v", err)
	}

	fmt.Printf("   📌 调用 %s(%v)\n", toolCall.Function.Name, args)

	result, err := tool.Execute(args)
	if err != nil {
		return fmt.Sprintf("工具执行失败: %v", err)
	}

	fmt.Printf("   📋 结果: %s\n", result)
	return result
}