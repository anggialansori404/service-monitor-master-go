package ws

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// WriteWait is the time allowed to write a message to the peer.
	WriteWait = 10 * time.Second

	// PongWait is the time allowed to read the next pong message from the peer.
	PongWait = 60 * time.Second

	// PingPeriod is the interval to send pings to peer. Must be less than PongWait.
	PingPeriod = (PongWait * 9) / 10

	// MaxMessageSize is the maximum message size allowed from peer (1MB).
	MaxMessageSize = 1024 * 1024

	// SendBufferSize is the capacity of the outgoing message buffer channel.
	SendBufferSize = 256
)

// Client represents an active WebSocket connection from an Agent.
type Client struct {
	// AgentID identifies which agent this connection belongs to.
	AgentID string

	// Conn is the underlying WebSocket connection.
	Conn *websocket.Conn

	// send is a buffered channel for outgoing messages.
	send chan []byte

	// done signals connection termination to internal pumps.
	done chan struct{}

	// closeOnce ensures connection is closed only once cleanly.
	closeOnce sync.Once

	// Pending holds channels waiting for request-response matching.
	// Key is the request ID, value is a channel that receives the response bytes.
	Pending map[string]chan []byte
	pendMu  sync.RWMutex
}

// RestartMessage is the JSON payload sent to an Agent to restart a service.
type RestartMessage struct {
	ReqID   string `json:"request_id"`
	Action  string `json:"action"`
	Service string `json:"service"`
}

// NewClient creates a new Client for the given agent and connection.
func NewClient(agentID string, conn *websocket.Conn) *Client {
	return &Client{
		AgentID: agentID,
		Conn:    conn,
		send:    make(chan []byte, SendBufferSize),
		done:    make(chan struct{}),
		Pending: make(map[string]chan []byte),
	}
}

// StartWritePump pumps messages from the send channel to the WebSocket connection.
// A dedicated goroutine with StartWritePump is started for each connection.
// It also periodically transmits Ping control frames.
func (c *Client) StartWritePump() {
	ticker := time.NewTicker(PingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			_ = c.Conn.SetWriteDeadline(time.Now().Add(WriteWait))
			if !ok {
				_ = c.Conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				return
			}

			w, err := c.Conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			if _, err := w.Write(message); err != nil {
				return
			}
			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			_ = c.Conn.SetWriteDeadline(time.Now().Add(WriteWait))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}

		case <-c.done:
			return
		}
	}
}

// SendMessage queues raw bytes for transmission via the write pump.
// It is non-blocking and safe for concurrent calls.
func (c *Client) SendMessage(data []byte) error {
	select {
	case <-c.done:
		return fmt.Errorf("client %s connection is closed", c.AgentID)
	case c.send <- data:
		return nil
	default:
		return fmt.Errorf("client %s outgoing send buffer full", c.AgentID)
	}
}

// WriteJSON serializes the value to JSON and queues it for transmission.
// It is thread-safe and non-blocking.
func (c *Client) WriteJSON(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}
	return c.SendMessage(data)
}

// Close gracefully closes the WebSocket connection.
func (c *Client) Close() error {
	var err error
	c.closeOnce.Do(func() {
		close(c.done)
		if c.Conn != nil {
			_ = c.Conn.WriteControl(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
				time.Now().Add(5*time.Second),
			)
			err = c.Conn.Close()
		}
	})
	return err
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
	case <-c.done:
		c.CleanupPending(reqID)
		return nil, fmt.Errorf("client disconnected while waiting for request %s", reqID)
	}
}
