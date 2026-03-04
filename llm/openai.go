package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

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

func (c *Client) Chat(messages []Message, tools []Tool) (*ChatResult, error) {
	reqBody := ChatRequest{
		Model:    c.Model,
		Messages: messages,
		Tools:    tools,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("序列化失败: %w", err)
	}

	req, err := http.NewRequest("POST", c.BaseURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
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
		return &ChatResult{
			ToolCalls:  choice.Message.ToolCalls,
			IsToolCall: true,
		}, nil
	}

	return &ChatResult{
		Content:    choice.Message.Content,
		IsToolCall: false,
	}, nil
}