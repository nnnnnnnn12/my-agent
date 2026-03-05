package api

import (
	"encoding/json"
	"fmt"
	"my-agent/agent"
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

func NewServer(dbPath string) (*Server, error) {
	gin.SetMode(gin.ReleaseMode)
	sessionStore, err := store.NewSessionStore(dbPath)
	if err != nil {
		return nil, fmt.Errorf("初始化数据库失败: %w", err)
	}
	s := &Server{
		router:       gin.New(),
		sessions:     make(map[string]*agent.ReActAgent),
		sessionStore: sessionStore,
	}
	s.setupMiddleware()
	s.setupRoutes()
	return s, nil
}

func (s *Server) setupMiddleware() {
	s.router.Use(gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
		return fmt.Sprintf("[%s] %s %s %d %s\n",
			param.TimeStamp.Format("15:04:05"),
			param.Method, param.Path,
			param.StatusCode, param.Latency,
		)
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
	s.router.GET("/chat/stream", s.handleChatStream) // SSE流式接口
	s.router.GET("/sessions", s.handleListSessions)
	s.router.DELETE("/sessions/:id", s.handleDeleteSession)
}

func (s *Server) Run(addr string) error {
	fmt.Printf("🚀 Agent HTTP服务启动: http://%s\n", addr)
	fmt.Printf("   POST   /chat            普通对话\n")
	fmt.Printf("   GET    /chat/stream     流式对话(SSE)\n")
	fmt.Printf("   GET    /health          健康检查\n")
	fmt.Printf("   GET    /sessions        查看所有会话\n")
	fmt.Printf("   DELETE /sessions/:id   删除会话\n\n")
	return s.router.Run(addr)
}

func (s *Server) ServeStatic(frontendDir string) {
	s.router.Static("/static", frontendDir)
	s.router.StaticFile("/", frontendDir+"/index.html")
}

func (s *Server) handleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"time":   time.Now().Format("2006-01-02 15:04:05"),
	})
}

func (s *Server) handleChat(c *gin.Context) {
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ChatResponse{Error: "请求格式错误: " + err.Error()})
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

// handleChatStream SSE流式接口
// 前端通过 EventSource 连接，实时接收Agent的思考过程和回复
func (s *Server) handleChatStream(c *gin.Context) {
	message := c.Query("message")
	sessionID := c.Query("session_id")
	if message == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少message参数"})
		return
	}
	if sessionID == "" {
		sessionID = fmt.Sprintf("sess_%d", time.Now().UnixNano())
	}

	// 设置SSE响应头
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no") // 禁止nginx缓冲，保证实时推送

	ag, err := s.getOrRestoreAgent(sessionID)
	if err != nil {
		sendSSE(c, "error", map[string]string{"message": err.Error()})
		return
	}

	// 先把sessionID发给前端
	sendSSE(c, "session", map[string]string{"session_id": sessionID})
	c.Writer.Flush()

	// 注册流式回调：Agent每有进展就推送给前端
	ag.Verbose = false
	ag.SetStreamCallback(func(event agent.StreamEvent) {
		sendSSE(c, string(event.Type), event.Data)
		c.Writer.Flush()
	})

	// 执行任务
	reply, err := ag.Run(message)
	if err != nil {
		sendSSE(c, "error", map[string]string{"message": err.Error()})
		c.Writer.Flush()
		return
	}

	// 发送最终完整回复
	sendSSE(c, "done", map[string]string{"reply": reply, "session_id": sessionID})
	c.Writer.Flush()

	// 持久化
	s.mu.Lock()
	s.sessions[sessionID] = ag
	s.mu.Unlock()
	s.sessionStore.SaveSession(sessionID, ag.GetMessages())
}

// sendSSE 发送一条SSE事件
func sendSSE(c *gin.Context, event string, data interface{}) {
	jsonData, _ := json.Marshal(data)
	fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event, string(jsonData))
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
	systemPrompt := `你是一个专业的AI助手GoAgent，通过HTTP API提供服务。
你有calculator、web_search和file_writer工具可以使用。
回复简洁专业，使用中文。`
	ag := agent.NewReActAgent(systemPrompt)
	ag.RegisterTool(tools.NewCalculatorTool())
	ag.RegisterTool(tools.NewTavilyTool())
	ag.RegisterTool(tools.NewFileTool("./output"))
	return ag
}