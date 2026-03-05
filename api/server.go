package api

import (
	"encoding/json"
	"fmt"
	"io"
	"my-agent/agent"
	"my-agent/rag"
	"my-agent/store"
	"my-agent/tools"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type Server struct {
	router       *gin.Engine
	sessions     map[string]*agent.ReActAgent
	sessionStore *store.SessionStore
	ragEngine    *rag.Engine
	mu           sync.RWMutex
}

type ChatRequest struct {
	Message   string `json:"message" binding:"required"`
	SessionID string `json:"session_id"`
}

type ChatResponse struct {
	Reply     string `json:"reply"`
	SessionID string `json:"session_id"`
	Error     string `json:"error,omitempty"`
}

func NewServer(dbPath string, ragStorePath string) (*Server, error) {
	gin.SetMode(gin.ReleaseMode)
	sessionStore, err := store.NewSessionStore(dbPath)
	if err != nil {
		return nil, fmt.Errorf("初始化数据库失败: %w", err)
	}
	s := &Server{
		router:       gin.New(),
		sessions:     make(map[string]*agent.ReActAgent),
		sessionStore: sessionStore,
		ragEngine:    rag.NewEngine(ragStorePath),
	}
	s.setupMiddleware()
	s.setupRoutes()
	return s, nil
}

func (s *Server) setupMiddleware() {
	s.router.Use(gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
		return fmt.Sprintf("[%s] %s %s %d %s\n",
			param.TimeStamp.Format("15:04:05"), param.Method,
			param.Path, param.StatusCode, param.Latency)
	}))
	s.router.Use(gin.Recovery())
	s.router.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})
}

func (s *Server) setupRoutes() {
	s.router.GET("/health", s.handleHealth)
	s.router.POST("/chat", s.handleChat)
	s.router.GET("/chat/stream", s.handleChatStream)
	s.router.GET("/sessions", s.handleListSessions)
	s.router.DELETE("/sessions/:id", s.handleDeleteSession)
	// RAG接口
	s.router.POST("/rag/upload", s.handleRAGUpload)
	s.router.GET("/rag/docs", s.handleRAGListDocs)
	s.router.DELETE("/rag/docs/:id", s.handleRAGDeleteDoc)
	s.router.GET("/rag/stats", s.handleRAGStats)
}

func (s *Server) Run(addr string) error {
	fmt.Printf("🚀 GoAgent服务启动: http://%s\n", addr)
	stats := s.ragEngine.Stats()
	fmt.Printf("📚 知识库：%d篇文档，%d个chunk\n\n", stats["docs"], stats["chunks"])
	return s.router.Run(addr)
}

func (s *Server) ServeStatic(frontendDir string) {
	s.router.Static("/static", frontendDir)
	s.router.StaticFile("/", frontendDir+"/index.html")
}

func (s *Server) handleHealth(c *gin.Context) {
	stats := s.ragEngine.Stats()
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"time":   time.Now().Format("2006-01-02 15:04:05"),
		"rag":    stats,
	})
}

func (s *Server) handleChat(c *gin.Context) {
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ChatResponse{Error: err.Error()})
		return
	}
	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = fmt.Sprintf("sess_%d", time.Now().UnixNano())
	}
	ag, err := s.getOrRestoreAgent(sessionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ChatResponse{Error: err.Error()})
		return
	}
	ag.Verbose = false
	reply, err := ag.Run(req.Message)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ChatResponse{Error: err.Error(), SessionID: sessionID})
		return
	}
	s.mu.Lock()
	s.sessions[sessionID] = ag
	s.mu.Unlock()
	s.sessionStore.SaveSession(sessionID, ag.GetMessages())
	c.JSON(http.StatusOK, ChatResponse{Reply: reply, SessionID: sessionID})
}

func (s *Server) handleChatStream(c *gin.Context) {
	message := c.Query("message")
	sessionID := c.Query("session_id")
	if message == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少message"})
		return
	}
	if sessionID == "" {
		sessionID = fmt.Sprintf("sess_%d", time.Now().UnixNano())
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	ag, err := s.getOrRestoreAgent(sessionID)
	if err != nil {
		sendSSE(c, "error", map[string]string{"message": err.Error()})
		return
	}
	sendSSE(c, "session", map[string]string{"session_id": sessionID})
	c.Writer.Flush()

	ag.Verbose = false
	ag.SetStreamCallback(func(event agent.StreamEvent) {
		sendSSE(c, string(event.Type), event.Data)
		c.Writer.Flush()
	})

	reply, err := ag.Run(message)
	if err != nil {
		sendSSE(c, "error", map[string]string{"message": err.Error()})
		c.Writer.Flush()
		return
	}
	sendSSE(c, "done", map[string]string{"reply": reply, "session_id": sessionID})
	c.Writer.Flush()

	s.mu.Lock()
	s.sessions[sessionID] = ag
	s.mu.Unlock()
	s.sessionStore.SaveSession(sessionID, ag.GetMessages())
}

func sendSSE(c *gin.Context, event string, data interface{}) {
	jsonData, _ := json.Marshal(data)
	fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event, string(jsonData))
}

// ===== RAG接口 =====

func (s *Server) handleRAGUpload(c *gin.Context) {
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择文件"})
		return
	}
	defer file.Close()

	// 只支持txt和md
	name := header.Filename
	if len(name) < 4 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "只支持 .txt 和 .md 文件"})
		return
	}
	ext := name[len(name)-3:]
	if ext != "txt" && ext != ".md" && name[len(name)-4:] != ".txt" {
		// 简单检查
	}

	content, err := io.ReadAll(file)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取文件失败"})
		return
	}

	docID := fmt.Sprintf("doc_%d", time.Now().UnixNano())
	if err := s.ragEngine.AddDocument(docID, name, string(content)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 让所有现有Agent知道有新文档（清理内存中的session，下次重新创建）
	s.mu.Lock()
	s.sessions = make(map[string]*agent.ReActAgent)
	s.mu.Unlock()

	c.JSON(http.StatusOK, gin.H{
		"message": fmt.Sprintf("文档 '%s' 已成功加入知识库", name),
		"doc_id":  docID,
	})
}

func (s *Server) handleRAGListDocs(c *gin.Context) {
	docs := s.ragEngine.ListDocuments()
	c.JSON(http.StatusOK, gin.H{"count": len(docs), "docs": docs})
}

func (s *Server) handleRAGDeleteDoc(c *gin.Context) {
	id := c.Param("id")
	if !s.ragEngine.DeleteDocument(id) {
		c.JSON(http.StatusNotFound, gin.H{"error": "文档不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已删除"})
}

func (s *Server) handleRAGStats(c *gin.Context) {
	c.JSON(http.StatusOK, s.ragEngine.Stats())
}

func (s *Server) getOrRestoreAgent(sessionID string) (*agent.ReActAgent, error) {
	s.mu.RLock()
	ag, exists := s.sessions[sessionID]
	s.mu.RUnlock()
	if exists {
		return ag, nil
	}
	messages, err := s.sessionStore.LoadSession(sessionID)
	if err != nil {
		return nil, fmt.Errorf("恢复会话失败: %w", err)
	}
	ag = s.newAgent()
	if len(messages) > 0 {
		ag.RestoreMessages(messages)
	}
	return ag, nil
}

func (s *Server) newAgent() *agent.ReActAgent {
	systemPrompt := `你是一个专业的AI助手GoAgent。
你有以下工具：
- calculator：数学计算
- web_search：搜索互联网实时信息
- knowledge_search：检索本地知识库文档
- file_writer：保存内容到文件

优先使用knowledge_search回答关于已上传文档的问题。
需要实时信息时用web_search。需要计算时用calculator。
所有回复使用中文。`

	ag := agent.NewReActAgent(systemPrompt)
	ag.RegisterTool(tools.NewCalculatorTool())
	ag.RegisterTool(tools.NewTavilyTool())
	ag.RegisterTool(tools.NewRAGSearchTool(s.ragEngine))
	ag.RegisterTool(tools.NewFileTool("./output"))
	return ag
}

func (s *Server) handleListSessions(c *gin.Context) {
	sessions, err := s.sessionStore.ListSessions()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"count": len(sessions), "sessions": sessions})
}

func (s *Server) handleDeleteSession(c *gin.Context) {
	id := c.Param("id")
	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()
	if err := s.sessionStore.DeleteSession(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "会话已删除"})
}