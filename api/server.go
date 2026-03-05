package api

import (
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
	sessions     map[string]*agent.ReActAgent // 内存中的Agent实例（运行时用）
	sessionStore *store.SessionStore          // SQLite持久化
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

	// 初始化SQLite
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
	s.router.GET("/sessions", s.handleListSessions)
	s.router.DELETE("/sessions/:id", s.handleDeleteSession)
}

func (s *Server) Run(addr string) error {
	fmt.Printf("🚀 Agent HTTP服务启动: http://%s\n", addr)
	fmt.Printf("   POST   /chat            发送消息\n")
	fmt.Printf("   GET    /health          健康检查\n")
	fmt.Printf("   GET    /sessions        查看所有会话\n")
	fmt.Printf("   DELETE /sessions/:id   删除会话\n\n")
	return s.router.Run(addr)
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

	// 生成或使用已有sessionID
	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = fmt.Sprintf("sess_%d", time.Now().UnixNano())
	}

	// 获取Agent（优先从内存，其次从数据库恢复）
	ag, err := s.getOrRestoreAgent(sessionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ChatResponse{Error: err.Error()})
		return
	}

	ag.Verbose = false
	reply, err := ag.Run(req.Message)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ChatResponse{
			Error: err.Error(), SessionID: sessionID,
		})
		return
	}

	// 持久化到数据库
	s.mu.Lock()
	s.sessions[sessionID] = ag
	s.mu.Unlock()

	if err := s.sessionStore.SaveSession(sessionID, ag.GetMessages()); err != nil {
		fmt.Printf("⚠️  会话持久化失败: %v\n", err)
	}

	c.JSON(http.StatusOK, ChatResponse{
		Reply:     reply,
		SessionID: sessionID,
	})
}

func (s *Server) handleListSessions(c *gin.Context) {
	sessions, err := s.sessionStore.ListSessions()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"count":    len(sessions),
		"sessions": sessions,
	})
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

// getOrRestoreAgent 优先从内存获取Agent，否则从数据库恢复历史
func (s *Server) getOrRestoreAgent(sessionID string) (*agent.ReActAgent, error) {
	// 先查内存
	s.mu.RLock()
	ag, exists := s.sessions[sessionID]
	s.mu.RUnlock()
	if exists {
		return ag, nil
	}

	// 从数据库加载历史消息
	messages, err := s.sessionStore.LoadSession(sessionID)
	if err != nil {
		return nil, fmt.Errorf("恢复会话失败: %w", err)
	}

	// 创建新Agent，注入历史消息
	ag = s.newAgent()
	if len(messages) > 0 {
		ag.RestoreMessages(messages)
		fmt.Printf("📂 已从数据库恢复会话 %s（%d 条消息）\n", sessionID, len(messages))
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