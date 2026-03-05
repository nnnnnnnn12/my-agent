package agent

import (
	"encoding/json"
	"fmt"
	"my-agent/llm"
	"my-agent/tools"
	"strings"
	"sync"
)

type StreamEventType string

const (
	EventThinking   StreamEventType = "thinking"
	EventToolCall   StreamEventType = "tool_call"
	EventToolResult StreamEventType = "tool_result"
	EventToken      StreamEventType = "token"
	EventStep       StreamEventType = "step"
)

type StreamEvent struct {
	Type StreamEventType
	Data map[string]string
}

type StreamCallback func(StreamEvent)

type ReActAgent struct {
	llmClient      *llm.Client
	memory         *Memory
	tools          map[string]tools.Tool
	toolDefs       []llm.Tool
	MaxSteps       int
	Verbose        bool
	streamCallback StreamCallback
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

func (a *ReActAgent) SetStreamCallback(cb StreamCallback) {
	a.streamCallback = cb
}

func (a *ReActAgent) emit(t StreamEventType, data map[string]string) {
	if a.streamCallback != nil {
		a.streamCallback(StreamEvent{Type: t, Data: data})
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

	a.emit(EventThinking, map[string]string{"message": "正在分析任务..."})
	a.memory.AddUserMessage(task)

	for step := 1; step <= a.MaxSteps; step++ {
		if a.Verbose {
			fmt.Printf("\n[步骤 %d]\n", step)
		}
		a.emit(EventStep, map[string]string{"step": fmt.Sprintf("%d", step)})

		// 用非流式调用判断是否需要工具
		result, err := a.llmClient.Chat(a.memory.GetMessages(), a.toolDefs)
		if err != nil {
			return "", fmt.Errorf("步骤%d失败: %w", step, err)
		}

		// ===== 情况A：不需要工具，用流式输出最终回复 =====
		if !result.IsToolCall {
			if a.Verbose {
				fmt.Printf("💡 流式输出最终回复...\n")
			}
			a.emit(EventThinking, map[string]string{"message": "正在生成回复..."})

			// 把当前messages发给流式接口，实时推token给前端
			fullReply, err := a.llmClient.ChatStream(
				a.memory.GetMessages(),
				func(token string) {
					// 每个token实时推给前端
					a.emit(EventToken, map[string]string{"text": token})
					if a.Verbose {
						fmt.Print(token) // CLI模式也实时打印
					}
				},
			)
			if err != nil {
				return "", fmt.Errorf("流式输出失败: %w", err)
			}
			if a.Verbose {
				fmt.Println()
				a.printDivider()
				fmt.Printf("✅ 任务完成（共 %d 步）\n", step)
				a.printDivider()
			}

			a.memory.AddAssistantMessage(fullReply)
			return fullReply, nil
		}

		// ===== 情况B：需要调用工具 =====
		if a.Verbose {
			fmt.Printf("🔧 调用 %d 个工具", len(result.ToolCalls))
			if len(result.ToolCalls) > 1 {
				fmt.Printf("（并发）")
			}
			fmt.Println()
		}

		for _, tc := range result.ToolCalls {
			a.emit(EventToolCall, map[string]string{
				"tool": tc.Function.Name,
				"args": tc.Function.Arguments,
				"id":   tc.ID,
			})
		}

		a.memory.AddToolCallIntent(result.ToolCalls)

		observations := a.executeToolsConcurrently(result.ToolCalls)
		for _, obs := range observations {
			a.memory.AddToolResult(obs.id, obs.name, obs.result)
			a.emit(EventToolResult, map[string]string{
				"tool":   obs.name,
				"result": obs.result,
				"id":     obs.id,
			})
		}

		a.emit(EventThinking, map[string]string{"message": "整合结果，继续推理..."})
	}

	return "", fmt.Errorf("超过最大步数(%d)", a.MaxSteps)
}

type toolObservation struct {
	id, name, result string
	index            int
}

func (a *ReActAgent) executeToolsConcurrently(toolCalls []llm.ToolCall) []toolObservation {
	results := make([]toolObservation, len(toolCalls))
	var wg sync.WaitGroup
	for i, tc := range toolCalls {
		wg.Add(1)
		go func(idx int, t llm.ToolCall) {
			defer wg.Done()
			var args map[string]interface{}
			json.Unmarshal([]byte(t.Function.Arguments), &args)
			if a.Verbose {
				fmt.Printf("   ⚡ [g%d] %s(%s)\n", idx+1, t.Function.Name, formatArgs(args))
			}
			obs := a.executeTool(t)
			if a.Verbose {
				preview := obs
				if len(preview) > 120 {
					preview = preview[:120] + "..."
				}
				fmt.Printf("   👁  [g%d] %s\n", idx+1, preview)
			}
			results[idx] = toolObservation{id: t.ID, name: t.Function.Name, result: obs, index: idx}
		}(i, tc)
	}
	wg.Wait()
	return results
}

func (a *ReActAgent) executeTool(tc llm.ToolCall) string {
	tool, ok := a.tools[tc.Function.Name]
	if !ok {
		return fmt.Sprintf("工具 '%s' 不存在，可用: %s", tc.Function.Name, a.availableToolNames())
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		return fmt.Sprintf("参数解析失败: %v", err)
	}
	res, err := tool.Execute(args)
	if err != nil {
		return fmt.Sprintf("执行出错: %v", err)
	}
	return res
}

func (a *ReActAgent) availableToolNames() string {
	names := make([]string, 0, len(a.tools))
	for n := range a.tools {
		names = append(names, n)
	}
	return strings.Join(names, ", ")
}

func (a *ReActAgent) printDivider() { fmt.Println("─────────────────────────────────────") }

func formatArgs(args map[string]interface{}) string {
	var parts []string
	for k, v := range args {
		s := fmt.Sprintf("%v", v)
		if len(s) > 50 {
			s = s[:50] + "..."
		}
		parts = append(parts, fmt.Sprintf("%s=%q", k, s))
	}
	return strings.Join(parts, ", ")
}

func (a *ReActAgent) GetMessages() []llm.Message        { return a.memory.GetMessages() }
func (a *ReActAgent) RestoreMessages(msgs []llm.Message) { a.memory.SetMessages(msgs) }