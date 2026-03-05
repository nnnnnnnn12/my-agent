package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"my-agent/llm"
	"time"

	_ "github.com/mattn/go-sqlite3" // 注册sqlite3驱动，必须import
)

// SessionStore 负责把对话历史持久化到SQLite
// 服务重启后会话不丢失
type SessionStore struct {
	db *sql.DB
}

// NewSessionStore 创建并初始化数据库
// dbPath: 数据库文件路径，例如 "./data/sessions.db"
func NewSessionStore(dbPath string) (*SessionStore, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}

	// 测试连接是否正常
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("数据库连接失败: %w", err)
	}

	store := &SessionStore{db: db}

	// 建表（如果不存在）
	if err := store.initSchema(); err != nil {
		return nil, fmt.Errorf("初始化表结构失败: %w", err)
	}

	fmt.Println("✅ SQLite 数据库已连接:", dbPath)
	return store, nil
}

// initSchema 创建数据库表结构
func (s *SessionStore) initSchema() error {
	// sessions 表：存储会话基本信息
	// messages 表：存储每条消息
	schema := `
	CREATE TABLE IF NOT EXISTS sessions (
		id         TEXT PRIMARY KEY,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS messages (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id  TEXT NOT NULL,
		role        TEXT NOT NULL,
		content     TEXT,
		tool_calls  TEXT,           -- JSON字符串，存储工具调用信息
		tool_call_id TEXT,
		name        TEXT,
		created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (session_id) REFERENCES sessions(id)
	);

	-- 加索引，按session_id查询更快
	CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id);
	`

	_, err := s.db.Exec(schema)
	return err
}

// SaveSession 保存或更新一个会话的完整消息历史
func (s *SessionStore) SaveSession(sessionID string, messages []llm.Message) error {
	// 用事务保证原子性：要么全部成功，要么全部回滚
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback() // 如果后面没有Commit，自动回滚

	// 插入或更新session记录
	_, err = tx.Exec(`
		INSERT INTO sessions(id, updated_at) VALUES(?, ?)
		ON CONFLICT(id) DO UPDATE SET updated_at = excluded.updated_at
	`, sessionID, time.Now())
	if err != nil {
		return fmt.Errorf("保存session失败: %w", err)
	}

	// 删除旧消息，重新插入（简单粗暴但可靠）
	_, err = tx.Exec("DELETE FROM messages WHERE session_id = ?", sessionID)
	if err != nil {
		return fmt.Errorf("清除旧消息失败: %w", err)
	}

	// 逐条插入消息
	for _, msg := range messages {
		// ToolCalls 序列化为JSON字符串存储
		toolCallsJSON := ""
		if len(msg.ToolCalls) > 0 {
			data, _ := json.Marshal(msg.ToolCalls)
			toolCallsJSON = string(data)
		}

		_, err = tx.Exec(`
			INSERT INTO messages(session_id, role, content, tool_calls, tool_call_id, name)
			VALUES(?, ?, ?, ?, ?, ?)
		`, sessionID, msg.Role, msg.Content, toolCallsJSON, msg.ToolCallID, msg.Name)
		if err != nil {
			return fmt.Errorf("插入消息失败: %w", err)
		}
	}

	return tx.Commit()
}

// LoadSession 从数据库加载一个会话的消息历史
// 如果会话不存在，返回空切片（不报错）
func (s *SessionStore) LoadSession(sessionID string) ([]llm.Message, error) {
	rows, err := s.db.Query(`
		SELECT role, content, tool_calls, tool_call_id, name
		FROM messages
		WHERE session_id = ?
		ORDER BY id ASC
	`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("查询消息失败: %w", err)
	}
	defer rows.Close()

	var messages []llm.Message
	for rows.Next() {
		var msg llm.Message
		var toolCallsJSON, toolCallID, name sql.NullString

		err := rows.Scan(&msg.Role, &msg.Content, &toolCallsJSON, &toolCallID, &name)
		if err != nil {
			return nil, fmt.Errorf("读取消息失败: %w", err)
		}

		// 反序列化 ToolCalls
		if toolCallsJSON.Valid && toolCallsJSON.String != "" {
			json.Unmarshal([]byte(toolCallsJSON.String), &msg.ToolCalls)
		}
		if toolCallID.Valid {
			msg.ToolCallID = toolCallID.String
		}
		if name.Valid {
			msg.Name = name.String
		}

		messages = append(messages, msg)
	}

	return messages, rows.Err()
}

// DeleteSession 删除一个会话及其所有消息
func (s *SessionStore) DeleteSession(sessionID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	tx.Exec("DELETE FROM messages WHERE session_id = ?", sessionID)
	tx.Exec("DELETE FROM sessions WHERE id = ?", sessionID)

	return tx.Commit()
}

// ListSessions 列出所有会话
func (s *SessionStore) ListSessions() ([]SessionInfo, error) {
	rows, err := s.db.Query(`
		SELECT s.id, s.created_at, s.updated_at, COUNT(m.id) as msg_count
		FROM sessions s
		LEFT JOIN messages m ON s.id = m.session_id
		GROUP BY s.id
		ORDER BY s.updated_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []SessionInfo
	for rows.Next() {
		var info SessionInfo
		rows.Scan(&info.ID, &info.CreatedAt, &info.UpdatedAt, &info.MessageCount)
		sessions = append(sessions, info)
	}
	return sessions, nil
}

// SessionInfo 会话基本信息
type SessionInfo struct {
	ID           string    `json:"id"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	MessageCount int       `json:"message_count"`
}

// Close 关闭数据库连接
func (s *SessionStore) Close() error {
	return s.db.Close()
}