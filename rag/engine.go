package rag

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Engine struct {
	embedder *Embedder
	store    *VectorStore
	chunker  *Chunker
}

func NewEngine(storePath string) *Engine {
	os.MkdirAll(filepath.Dir(storePath), 0755)
	return &Engine{
		embedder: NewEmbedder(),
		store:    NewVectorStore(storePath),
		chunker:  NewChunker(DefaultChunkConfig()),
	}
}

func (e *Engine) AddDocument(docID, docName, content string) error {
	fmt.Printf("📄 正在处理: %s\n", docName)
	chunks := e.chunker.Split(docID, docName, content)
	if len(chunks) == 0 {
		return fmt.Errorf("文档内容为空")
	}
	fmt.Printf("   切分为 %d 个chunk，向量化中...\n", len(chunks))

	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Content
	}
	vectors, err := e.embedder.EmbedBatch(texts)
	if err != nil {
		return fmt.Errorf("向量化失败: %w", err)
	}

	entries := make([]VectorEntry, len(chunks))
	for i := range chunks {
		entries[i] = VectorEntry{Chunk: chunks[i], Vector: vectors[i]}
	}

	e.store.AddDocument(Document{
		ID:         docID,
		Name:       docName,
		Size:       len(content),
		ChunkCount: len(chunks),
		UploadedAt: time.Now(),
	}, entries)
	return nil
}

func (e *Engine) Search(query string, topK int) (string, error) {
	queryVec, err := e.embedder.Embed(query)
	if err != nil {
		return "", fmt.Errorf("查询向量化失败: %w", err)
	}
	chunks := e.store.Search(queryVec, topK, 0.5)
	if len(chunks) == 0 {
		return "知识库中没有找到相关内容。", nil
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("从知识库检索到 %d 条相关内容：\n\n", len(chunks)))
	for i, chunk := range chunks {
		sb.WriteString(fmt.Sprintf("--- 片段%d（来源: %s，相似度: %.2f）---\n",
			i+1, chunk.DocName, chunk.Score))
		sb.WriteString(chunk.Content)
		sb.WriteString("\n\n")
	}
	return sb.String(), nil
}

func (e *Engine) DeleteDocument(docID string) bool { return e.store.DeleteDocument(docID) }
func (e *Engine) ListDocuments() []Document        { return e.store.ListDocuments() }
func (e *Engine) Stats() map[string]int            { return e.store.Stats() }