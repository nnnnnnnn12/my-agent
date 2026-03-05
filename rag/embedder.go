package rag

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
)

// Embedder 支持多个Embedding服务商
// 优先用硅基流动（免费），也支持OpenAI兼容格式
type Embedder struct {
	APIKey  string
	BaseURL string
	Model   string
}

func NewEmbedder() *Embedder {
	// 优先用硅基流动的Key
	apiKey := os.Getenv("SILICONFLOW_API_KEY")
	baseURL := "https://api.siliconflow.cn/v1/embeddings"
	model := "BAAI/bge-m3" // 免费，中英文效果很好

	// 如果没有硅基流动Key，尝试用OpenAI Key
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
		baseURL = "https://api.openai.com/v1/embeddings"
		model = "text-embedding-3-small"
	}

	if apiKey == "" {
		panic("请设置环境变量 SILICONFLOW_API_KEY（推荐）或 OPENAI_API_KEY")
	}

	return &Embedder{APIKey: apiKey, BaseURL: baseURL, Model: model}
}

type embedRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type embedResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (e *Embedder) Embed(text string) ([]float64, error) {
	reqBody := embedRequest{Model: e.Model, Input: text}
	jsonData, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", e.BaseURL, bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.APIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result embedResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析失败: %w，原始: %s", err, string(body))
	}
	if result.Error != nil {
		return nil, fmt.Errorf("API错误: %s", result.Error.Message)
	}
	if len(result.Data) == 0 {
		return nil, fmt.Errorf("返回空结果，原始响应: %s", string(body))
	}
	return result.Data[0].Embedding, nil
}

func (e *Embedder) EmbedBatch(texts []string) ([][]float64, error) {
	vectors := make([][]float64, len(texts))
	for i, text := range texts {
		vec, err := e.Embed(text)
		if err != nil {
			return nil, fmt.Errorf("第%d个文本失败: %w", i, err)
		}
		vectors[i] = vec
	}
	return vectors, nil
}

func CosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}