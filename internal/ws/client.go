package ws

import (
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Client represents an active WebSocket connection from an Agent.
type Client struct {
	// AgentID identifies which agent this connection belongs to.
	AgentID string

	// Conn is the underlying WebSocket connection.
	Conn *websocket.Conn

	// mu protects writes to the WebSocket connection.
	// WebSocket connections are not safe for concurrent writes.
	mu sync.Mutex

	// Pending holds channels waiting for request-response matching.
	// Key is the request ID, value is a channel that receives the response bytes.
	Pending map[string]chan []byte
	pendMu  sync.RWMutex
}

// RestartMessage is the JSON payload sent to an Agent to restart a service.
type RestartMessage struct {
	ReqID   string `json:"request_id"` // Pastikan nama JSON tag "request_id" agar agent mendapatkannya
	Action  string `json:"action"`
	Service string `json:"service"`
}

// NewClient creates a new Client for the given agent and connection.
func NewClient(agentID string, conn *websocket.Conn) *Client {
	return &Client{
		AgentID: agentID,
		Conn:    conn,
		Pending: make(map[string]chan []byte),
	}
}

// WriteJSON sends a JSON-encoded message to the client's WebSocket connection.
// It is safe for concurrent use.
func (c *Client) WriteJSON(v interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Conn.WriteJSON(v)
}

// Close gracefully closes the WebSocket connection by sending a close message
// and then closing the underlying connection.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Send a close message with a deadline so we don't block forever.
	_ = c.Conn.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
		time.Now().Add(5*time.Second),
	)
	return c.Conn.Close()
}

// RegisterPending creates a channel for a given request ID and stores it.
func (c *Client) RegisterPending(reqID string) chan []byte {
	ch := make(chan []byte, 1)
	c.pendMu.Lock()
	c.Pending[reqID] = ch
	c.pendMu.Unlock()
	return ch
}

// ResolvePending sends response bytes to the pending channel for the given
// request ID and removes it from the map. Returns false if no pending request found.
func (c *Client) ResolvePending(reqID string, data []byte) bool {
	c.pendMu.Lock()
	ch, ok := c.Pending[reqID]
	if ok {
		delete(c.Pending, reqID)
	}
	c.pendMu.Unlock()

	if ok {
		ch <- data
		return true
	}
	return false
}

// CleanupPending removes a pending request (e.g., on timeout).
func (c *Client) CleanupPending(reqID string) {
	c.pendMu.Lock()
	delete(c.Pending, reqID)
	c.pendMu.Unlock()
}

// SendRequest sends a message and waits for a matching response with a timeout.
func (c *Client) SendRequest(msg interface{}, reqID string, timeout time.Duration) ([]byte, error) {
	ch := c.RegisterPending(reqID)

	if err := c.WriteJSON(msg); err != nil {
		c.CleanupPending(reqID)
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	select {
	case resp := <-ch:
		return resp, nil
	case <-time.After(timeout):
		c.CleanupPending(reqID)
		return nil, fmt.Errorf("request %s timed out after %v", reqID, timeout)
	}
}
