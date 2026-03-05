package rag

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Chunk 文档片段
type Chunk struct {
	ID      string  `json:"id"`
	DocID   string  `json:"doc_id"`
	DocName string  `json:"doc_name"`
	Content string  `json:"content"`
	Index   int     `json:"index"`
	Score   float64 `json:"score,omitempty"`
}

// ChunkConfig 切片配置
type ChunkConfig struct {
	ChunkSize    int
	ChunkOverlap int
}

func DefaultChunkConfig() ChunkConfig {
	return ChunkConfig{ChunkSize: 500, ChunkOverlap: 50}
}

type Chunker struct{ config ChunkConfig }

func NewChunker(config ChunkConfig) *Chunker { return &Chunker{config: config} }

// Split 按段落切分，段落过长则按字符切分
func (c *Chunker) Split(docID, docName, content string) []Chunk {
	paragraphs := splitParagraphs(content)
	var chunks []Chunk
	var current strings.Builder
	chunkIndex := 0

	flush := func() {
		text := strings.TrimSpace(current.String())
		if text == "" {
			return
		}
		chunks = append(chunks, Chunk{
			ID:      fmt.Sprintf("%s_%d", docID, chunkIndex),
			DocID:   docID,
			DocName: docName,
			Content: text,
			Index:   chunkIndex,
		})
		chunkIndex++
		current.Reset()
	}

	for _, para := range paragraphs {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		if utf8.RuneCountInString(para) > c.config.ChunkSize {
			flush()
			sub := c.splitLong(docID, docName, para, chunkIndex)
			chunks = append(chunks, sub...)
			chunkIndex += len(sub)
			continue
		}
		currentLen := utf8.RuneCountInString(current.String())
		paraLen := utf8.RuneCountInString(para)
		if currentLen+paraLen > c.config.ChunkSize && currentLen > 0 {
			flush()
		}
		if current.Len() > 0 {
			current.WriteString("\n\n")
		}
		current.WriteString(para)
	}
	flush()
	return chunks
}

func (c *Chunker) splitLong(docID, docName, para string, startIdx int) []Chunk {
	runes := []rune(para)
	var chunks []Chunk
	i, idx := 0, startIdx
	for i < len(runes) {
		end := i + c.config.ChunkSize
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, Chunk{
			ID:      fmt.Sprintf("%s_%d", docID, idx),
			DocID:   docID,
			DocName: docName,
			Content: string(runes[i:end]),
			Index:   idx,
		})
		idx++
		next := end - c.config.ChunkOverlap
		if next <= i || next >= len(runes) {
			break
		}
		i = next
	}
	return chunks
}

func splitParagraphs(content string) []string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	var result []string
	for _, p := range strings.Split(content, "\n\n") {
		if p = strings.TrimSpace(p); p != "" {
			result = append(result, p)
		}
	}
	return result
}