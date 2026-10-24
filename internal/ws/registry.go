package ws

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// WsResponse represents a general response from the WebSocket agent.
type WsResponse map[string]interface{}

// Registry manages active WebSocket connections from Agents,
// mapping each AgentID to its connected Client.
type Registry struct {
	mu              sync.RWMutex
	clients         map[string]*Client
	PendingRequests sync.Map
}

// NewRegistry creates and returns a new, empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		clients: make(map[string]*Client),
	}
}

// Register adds a client to the registry. If an existing connection
// for the same AgentID exists, it is closed and replaced.
func (r *Registry) Register(client *Client) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// If there's already a connection for this agent, log a warning if from different remote address and close it.
	if existing, ok := r.clients[client.AgentID]; ok {
		if existing.Conn != nil && client.Conn != nil {
			oldAddr := existing.Conn.RemoteAddr().String()
			newAddr := client.Conn.RemoteAddr().String()
			if oldAddr != newAddr {
				log.Printf("[REGISTRY] Warning: Agent %s reconnected from %s, replacing previous connection from %s. Verify only one agent instance is running.", client.AgentID, newAddr, oldAddr)
			}
		}
		_ = existing.Close()
	}

	r.clients[client.AgentID] = client
}

// IsCurrentClient checks if the provided client pointer is the currently registered client for agentID.
func (r *Registry) IsCurrentClient(agentID string, client *Client) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	current, ok := r.clients[agentID]
	return ok && current == client
}

// UnregisterClient removes a client from the registry and closes its connection ONLY if it
// is still the currently active client for the given AgentID. This prevents race conditions
// where an older closing connection kills a newer reconnected client.
func (r *Registry) UnregisterClient(agentID string, client *Client) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if current, ok := r.clients[agentID]; ok {
		if client != nil && current != client {
			// A newer client has already registered, do not touch it
			return false
		}
		_ = current.Close()
		delete(r.clients, agentID)
		return true
	}
	return false
}

// Unregister removes whatever client is currently registered for the given AgentID and closes it.
func (r *Registry) Unregister(agentID string) {
	r.UnregisterClient(agentID, nil)
}

// SendToAgent sends a JSON-encoded message to the Agent connected
// for the given AgentID. Returns an error if no agent is connected
// or if the write fails.
func (r *Registry) SendToAgent(agentID string, message interface{}) error {
	r.mu.RLock()
	client, ok := r.clients[agentID]
	r.mu.RUnlock()

	if !ok {
		return fmt.Errorf("no agent connected for agent_id %s", agentID)
	}

	if err := client.WriteJSON(message); err != nil {
		return fmt.Errorf("failed to send message to agent %s: %w", agentID, err)
	}

	return nil
}

// SendRequestToAgent sends a message with a request ID to the agent and waits
// synchronously for a matching response. Returns the raw response bytes or an error on timeout.
func (r *Registry) SendRequestToAgent(agentID string, message interface{}, reqID string, timeout time.Duration) ([]byte, error) {
	r.mu.RLock()
	client, ok := r.clients[agentID]
	r.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("no agent connected for agent_id %s", agentID)
	}

	return client.SendRequest(message, reqID, timeout)
}

// GetClient returns the Client for the given AgentID, or nil if not connected.
func (r *Registry) GetClient(agentID string) *Client {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.clients[agentID]
}

// IsConnected returns true if an agent is connected for the given AgentID.
func (r *Registry) IsConnected(agentID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.clients[agentID]
	return ok
}

// ConnectedCount returns the number of currently connected agents.
func (r *Registry) ConnectedCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.clients)
}

// RestartRequest sends a restart command to an agent and waits synchronously for the response.
// Returns the response or an error on timeout or send failure.
// Caller is responsible for auditing/logging the result.
func (r *Registry) RequestRestart(agentID, serviceName string, timeout time.Duration) (WsResponse, error) {
	reqID := fmt.Sprintf("req-%d", time.Now().UnixNano())

	// Buffered so the non-blocking forward in the WS handler cannot drop a
	// reply that arrives before this goroutine starts waiting.
	respChan := make(chan WsResponse, 1)
	r.PendingRequests.Store(reqID, respChan)
	defer r.PendingRequests.Delete(reqID)

	payload := map[string]interface{}{
		"request_id": reqID,
		"action":     "restart",
		"service":    serviceName,
	}

	if err := r.SendToAgent(agentID, payload); err != nil {
		return nil, err
	}

	select {
	case resp := <-respChan:
		return resp, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout waiting for agent response")
	}
}
