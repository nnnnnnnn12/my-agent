package llm

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// ===== 数据结构 =====

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type Tool struct {
	Type     string         `json:"type"`
	Function FunctionDetail `json:"function"`
}

type FunctionDetail struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

type ChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Tools    []Tool    `json:"tools,omitempty"`
	Stream   bool      `json:"stream,omitempty"`
}

type ChatResponse struct {
	Choices []struct {
		Message      Message `json:"message"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type ChatResult struct {
	Content    string
	ToolCalls  []ToolCall
	IsToolCall bool
}

// StreamChunk 流式响应的单个chunk
type StreamChunk struct {
	Choices []struct {
		Delta struct {
			Content   string     `json:"content"`
			ToolCalls []ToolCall `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

// TokenCallback 流式token回调函数
type TokenCallback func(token string)

// ===== Client =====

type Client struct {
	APIKey  string
	BaseURL string
	Model   string
}

func NewClient() *Client {
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		panic("请设置环境变量 DEEPSEEK_API_KEY")
	}
	return &Client{
		APIKey:  apiKey,
		BaseURL: "https://api.deepseek.com/v1/chat/completions",
		Model:   "deepseek-chat",
	}
}

// Chat 非流式调用，用于工具调用阶段（工具调用不支持流式）
func (c *Client) Chat(messages []Message, tools []Tool) (*ChatResult, error) {
	reqBody := ChatRequest{
		Model:    c.Model,
		Messages: messages,
		Tools:    tools,
	}
	jsonData, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", c.BaseURL, bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var chatResp ChatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w，原始: %s", err, string(body))
	}
	if chatResp.Error != nil {
		return nil, fmt.Errorf("API错误: %s", chatResp.Error.Message)
	}
	if len(chatResp.Choices) == 0 {
		return nil, fmt.Errorf("空响应: %s", string(body))
	}

	choice := chatResp.Choices[0]
	if choice.FinishReason == "tool_calls" || len(choice.Message.ToolCalls) > 0 {
		return &ChatResult{ToolCalls: choice.Message.ToolCalls, IsToolCall: true}, nil
	}
	return &ChatResult{Content: choice.Message.Content, IsToolCall: false}, nil
}

// ChatStream 真正的流式调用
// 每收到一个token就调用 onToken 回调
// 返回完整的回复内容（用于存入memory）
func (c *Client) ChatStream(messages []Message, onToken TokenCallback) (string, error) {
	reqBody := ChatRequest{
		Model:    c.Model,
		Messages: messages,
		Stream:   true, // 开启流式
	}
	jsonData, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", c.BaseURL, bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 流式响应是一行一行的SSE格式：
	// data: {"choices":[{"delta":{"content":"你好"},...}]}
	// data: [DONE]
	var fullContent strings.Builder
	scanner := bufio.NewScanner(resp.Body)

	for scanner.Scan() {
		line := scanner.Text()

		// 跳过空行
		if line == "" {
			continue
		}
		// 结束标志
		if line == "data: [DONE]" {
			break
		}
		// 去掉 "data: " 前缀
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		jsonStr := strings.TrimPrefix(line, "data: ")

		var chunk StreamChunk
		if err := json.Unmarshal([]byte(jsonStr), &chunk); err != nil {
			continue // 跳过解析失败的行
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		token := chunk.Choices[0].Delta.Content
		if token != "" {
			fullContent.WriteString(token)
			if onToken != nil {
				onToken(token) // 实时推送给调用方
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fullContent.String(), fmt.Errorf("读取流失败: %w", err)
	}

	return fullContent.String(), nil
}