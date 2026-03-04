package agent

import "my-agent/llm"

type Memory struct {
	messages []llm.Message
}

func NewMemory(systemPrompt string) *Memory {
	m := &Memory{}
	if systemPrompt != "" {
		m.messages = append(m.messages, llm.Message{
			Role:    "system",
			Content: systemPrompt,
		})
	}
	return m
}

func (m *Memory) AddUserMessage(content string) {
	m.messages = append(m.messages, llm.Message{Role: "user", Content: content})
}

func (m *Memory) AddAssistantMessage(content string) {
	m.messages = append(m.messages, llm.Message{Role: "assistant", Content: content})
}

// AddToolCallIntent 记录LLM"打算调用工具"这个意图
// OpenAI格式要求：工具结果必须紧跟在这条消息后面
func (m *Memory) AddToolCallIntent(toolCalls []llm.ToolCall) {
	m.messages = append(m.messages, llm.Message{
		Role:      "assistant",
		ToolCalls: toolCalls,
	})
}

// AddToolResult 记录工具执行结果
func (m *Memory) AddToolResult(toolCallID, toolName, result string) {
	m.messages = append(m.messages, llm.Message{
		Role:       "tool",
		ToolCallID: toolCallID,
		Name:       toolName,
		Content:    result,
	})
}

func (m *Memory) GetMessages() []llm.Message {
	return m.messages
}

func (m *Memory) Clear() {
	if len(m.messages) > 0 && m.messages[0].Role == "system" {
		m.messages = m.messages[:1]
	} else {
		m.messages = nil
	}
}