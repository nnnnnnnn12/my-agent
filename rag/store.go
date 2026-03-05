package rag

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"
)

// Document 已上传的文档元信息
type Document struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Size       int       `json:"size"`
	ChunkCount int       `json:"chunk_count"`
	UploadedAt time.Time `json:"uploaded_at"`
}

// VectorEntry 向量存储条目
type VectorEntry struct {
	Chunk  Chunk     `json:"chunk"`
	Vector []float64 `json:"vector"`
}

// VectorStore 内存向量存储，JSON文件持久化
type VectorStore struct {
	mu        sync.RWMutex
	docs      map[string]Document // docID → Document
	entries   []VectorEntry       // 所有chunk的向量
	storePath string              // 持久化文件路径
}

// persistData 持久化的数据结构
type persistData struct {
	Docs    map[string]Document `json:"docs"`
	Entries []VectorEntry       `json:"entries"`
}

func NewVectorStore(storePath string) *VectorStore {
	vs := &VectorStore{
		docs:      make(map[string]Document),
		storePath: storePath,
	}
	vs.load() // 启动时从文件恢复
	return vs
}

// AddDocument 添加文档的所有chunk向量
func (vs *VectorStore) AddDocument(doc Document, entries []VectorEntry) {
	vs.mu.Lock()
	defer vs.mu.Unlock()

	vs.docs[doc.ID] = doc
	vs.entries = append(vs.entries, entries...)
	vs.save()
	fmt.Printf("📚 文档 '%s' 已加入知识库（%d 个chunk）\n", doc.Name, len(entries))
}

// Search 检索最相关的TopK个chunk
func (vs *VectorStore) Search(queryVec []float64, topK int, threshold float64) []Chunk {
	vs.mu.RLock()
	defer vs.mu.RUnlock()

	if len(vs.entries) == 0 {
		return nil
	}

	// 计算所有chunk与query的相似度
	type scored struct {
		chunk Chunk
		score float64
	}
	var results []scored

	for _, entry := range vs.entries {
		score := CosineSimilarity(queryVec, entry.Vector)
		if score >= threshold {
			c := entry.Chunk
			c.Score = score
			results = append(results, scored{c, score})
		}
	}

	// 按相似度降序排列
	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	// 取TopK
	if topK > len(results) {
		topK = len(results)
	}
	chunks := make([]Chunk, topK)
	for i := 0; i < topK; i++ {
		chunks[i] = results[i].chunk
	}
	return chunks
}

// DeleteDocument 删除指定文档的所有chunk
func (vs *VectorStore) DeleteDocument(docID string) bool {
	vs.mu.Lock()
	defer vs.mu.Unlock()

	if _, exists := vs.docs[docID]; !exists {
		return false
	}

	delete(vs.docs, docID)

	// 过滤掉该文档的所有entry
	var remaining []VectorEntry
	for _, e := range vs.entries {
		if e.Chunk.DocID != docID {
			remaining = append(remaining, e)
		}
	}
	vs.entries = remaining
	vs.save()
	return true
}

// ListDocuments 返回所有文档列表
func (vs *VectorStore) ListDocuments() []Document {
	vs.mu.RLock()
	defer vs.mu.RUnlock()

	docs := make([]Document, 0, len(vs.docs))
	for _, d := range vs.docs {
		docs = append(docs, d)
	}
	return docs
}

// Stats 返回知识库统计信息
func (vs *VectorStore) Stats() map[string]int {
	vs.mu.RLock()
	defer vs.mu.RUnlock()
	return map[string]int{
		"docs":   len(vs.docs),
		"chunks": len(vs.entries),
	}
}

// save 持久化到JSON文件
func (vs *VectorStore) save() {
	if vs.storePath == "" {
		return
	}
	data := persistData{Docs: vs.docs, Entries: vs.entries}
	jsonData, err := json.Marshal(data)
	if err != nil {
		fmt.Printf("⚠️  向量存储序列化失败: %v\n", err)
		return
	}
	if err := os.WriteFile(vs.storePath, jsonData, 0644); err != nil {
		fmt.Printf("⚠️  向量存储写入失败: %v\n", err)
	}
}

// load 从JSON文件恢复
func (vs *VectorStore) load() {
	if vs.storePath == "" {
		return
	}
	data, err := os.ReadFile(vs.storePath)
	if err != nil {
		return // 文件不存在是正常的（首次启动）
	}
	var pd persistData
	if err := json.Unmarshal(data, &pd); err != nil {
		fmt.Printf("⚠️  向量存储加载失败: %v\n", err)
		return
	}
	if pd.Docs != nil {
		vs.docs = pd.Docs
	}
	vs.entries = pd.Entries
	fmt.Printf("📂 知识库已恢复：%d 个文档，%d 个chunk\n", len(vs.docs), len(vs.entries))
}