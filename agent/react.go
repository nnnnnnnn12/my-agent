package agent

import (
	"encoding/json"
	"fmt"
	"my-agent/llm"
	"my-agent/tools"
	"strings"
	"sync"
)

type ReActAgent struct {
	llmClient *llm.Client
	memory    *Memory
	tools     map[string]tools.Tool
	toolDefs  []llm.Tool
	MaxSteps  int
	Verbose   bool
}

func NewReActAgent(systemPrompt string) *ReActAgent {
	return &ReActAgent{
		llmClient: llm.NewClient(),
		memory:    NewMemory(systemPrompt),
		tools:     make(map[string]tools.Tool),
		MaxSteps:  10,
		Verbose:   true,
	}
}

func (a *ReActAgent) RegisterTool(t tools.Tool) {
	a.tools[t.Name()] = t
	a.toolDefs = append(a.toolDefs, llm.Tool{
		Type: "function",
		Function: llm.FunctionDetail{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.Parameters(),
		},
	})
}

func (a *ReActAgent) Run(task string) (string, error) {
	if a.Verbose {
		a.printDivider()
		fmt.Printf("📋 任务: %s\n", task)
		a.printDivider()
	}

	a.memory.AddUserMessage(task)

	for step := 1; step <= a.MaxSteps; step++ {
		if a.Verbose {
			fmt.Printf("\n[步骤 %d]\n", step)
		}

		result, err := a.llmClient.Chat(a.memory.GetMessages(), a.toolDefs)
		if err != nil {
			return "", fmt.Errorf("步骤%d LLM调用失败: %w", step, err)
		}

		// 情况A：直接给出答案
		if !result.IsToolCall {
			if a.Verbose {
				fmt.Printf("💡 思考完毕，给出答案\n")
				a.printDivider()
				fmt.Printf("✅ 任务完成（共 %d 步）\n", step)
				a.printDivider()
			}
			a.memory.AddAssistantMessage(result.Content)
			return result.Content, nil
		}

		// 情况B：需要调用工具
		if a.Verbose {
			fmt.Printf("🔧 需要调用 %d 个工具", len(result.ToolCalls))
			if len(result.ToolCalls) > 1 {
				fmt.Printf("（并发执行）")
			}
			fmt.Println()
		}

		a.memory.AddToolCallIntent(result.ToolCalls)

		// ===== 核心：并发执行所有工具 =====
		observations := a.executeToolsConcurrently(result.ToolCalls)

		// 按顺序把结果存入历史（顺序必须和toolCalls一致）
		for _, obs := range observations {
			a.memory.AddToolResult(obs.id, obs.name, obs.result)
		}
	}

	return "", fmt.Errorf("超过最大步数限制(%d)", a.MaxSteps)
}

// toolObservation 存储单个工具的执行结果
type toolObservation struct {
	id     string
	name   string
	result string
	index  int // 保持原始顺序
}

// executeToolsConcurrently 并发执行多个工具
// 这是Go goroutine的经典用法：WaitGroup + 带索引的结果收集
func (a *ReActAgent) executeToolsConcurrently(toolCalls []llm.ToolCall) []toolObservation {
	results := make([]toolObservation, len(toolCalls))
	var wg sync.WaitGroup

	for i, tc := range toolCalls {
		wg.Add(1)

		// 关键：用参数传入i和tc，避免闭包变量捕获问题
		go func(index int, toolCall llm.ToolCall) {
			defer wg.Done()

			var args map[string]interface{}
			json.Unmarshal([]byte(toolCall.Function.Arguments), &args)

			if a.Verbose {
				fmt.Printf("   ⚡ [goroutine %d] 调用 %s(%s)\n",
					index+1, toolCall.Function.Name, formatArgs(args))
			}

			observation := a.executeTool(toolCall)

			if a.Verbose {
				preview := observation
				if len(preview) > 150 {
					preview = preview[:150] + "..."
				}
				fmt.Printf("   👁  [goroutine %d] 结果: %s\n", index+1, preview)
			}

			// 写入对应位置，无需加锁（每个goroutine写不同index）
			results[index] = toolObservation{
				id:     toolCall.ID,
				name:   toolCall.Function.Name,
				result: observation,
				index:  index,
			}
		}(i, tc)
	}

	wg.Wait() // 等待所有goroutine完成
	return results
}

func (a *ReActAgent) executeTool(toolCall llm.ToolCall) string {
	tool, exists := a.tools[toolCall.Function.Name]
	if !exists {
		return fmt.Sprintf("错误：工具 '%s' 不存在，可用: %s",
			toolCall.Function.Name, a.availableToolNames())
	}

	var args map[string]interface{}
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		return fmt.Sprintf("参数解析失败: %v", err)
	}

	result, err := tool.Execute(args)
	if err != nil {
		return fmt.Sprintf("执行出错: %v", err)
	}
	return result
}

func (a *ReActAgent) availableToolNames() string {
	names := make([]string, 0, len(a.tools))
	for name := range a.tools {
		names = append(names, name)
	}
	return strings.Join(names, ", ")
}

func (a *ReActAgent) printDivider() {
	fmt.Println("─────────────────────────────────────")
}

func formatArgs(args map[string]interface{}) string {
	parts := []string{}
	for k, v := range args {
		val := fmt.Sprintf("%v", v)
		if len(val) > 60 {
			val = val[:60] + "..."
		}
		parts = append(parts, fmt.Sprintf("%s=%q", k, val))
	}
	return strings.Join(parts, ", ")
}

// GetMessages 返回当前完整对话历史（用于持久化）
func (a *ReActAgent) GetMessages() []llm.Message {
	return a.memory.GetMessages()
}

// RestoreMessages 从外部注入历史消息（用于从数据库恢复会话）
func (a *ReActAgent) RestoreMessages(messages []llm.Message) {
	a.memory.SetMessages(messages)
}