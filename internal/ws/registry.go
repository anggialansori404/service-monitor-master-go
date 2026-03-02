package ws

import (
	"fmt"
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

	// If there's already a connection for this agent, close it first.
	if existing, ok := r.clients[client.AgentID]; ok {
		_ = existing.Close()
	}

	r.clients[client.AgentID] = client
}

// Unregister removes a client from the registry and closes its connection.
// It is a no-op if no client is registered for the given AgentID.
func (r *Registry) Unregister(agentID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if client, ok := r.clients[agentID]; ok {
		_ = client.Close()
		delete(r.clients, agentID)
	}
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
