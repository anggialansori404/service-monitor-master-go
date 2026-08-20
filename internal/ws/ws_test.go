package ws

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

var testUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func setupTestWSServer(t *testing.T, onConnect func(client *Client)) (*httptest.Server, string) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade error: %v", err)
			return
		}
		client := NewClient("agent-123", conn)
		if onConnect != nil {
			onConnect(client)
		}
	}))

	wsURL := "ws" + strings.TrimPrefix(s.URL, "http")
	return s, wsURL
}

func TestClientWritePumpAndHeartbeat(t *testing.T) {
	doneCh := make(chan struct{})
	server, wsURL := setupTestWSServer(t, func(client *Client) {
		go client.StartWritePump()

		// Send test JSON
		err := client.WriteJSON(map[string]string{"action": "test", "hello": "world"})
		if err != nil {
			t.Errorf("WriteJSON error: %v", err)
		}

		// Wait a bit, then close
		time.Sleep(100 * time.Millisecond)
		_ = client.Close()
		close(doneCh)
	})
	defer server.Close()

	// Dial test server
	dialer := websocket.DefaultDialer
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial error: %v", err)
	}
	defer conn.Close()

	// Read message sent by writePump
	var msg map[string]string
	err = conn.ReadJSON(&msg)
	if err != nil {
		t.Fatalf("ReadJSON error: %v", err)
	}
	if msg["action"] != "test" || msg["hello"] != "world" {
		t.Errorf("unexpected message received: %+v", msg)
	}

	select {
	case <-doneCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for client close")
	}
}

func TestClientPendingResolution(t *testing.T) {
	client := NewClient("test-agent", nil)

	reqID := "req-abc-123"
	ch := client.RegisterPending(reqID)
	if ch == nil {
		t.Fatal("expected non-nil channel")
	}

	testPayload := []byte(`{"status":"success"}`)
	ok := client.ResolvePending(reqID, testPayload)
	if !ok {
		t.Fatal("expected ResolvePending to succeed")
	}

	select {
	case data := <-ch:
		if string(data) != string(testPayload) {
			t.Errorf("expected %s, got %s", testPayload, data)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for resolved pending data")
	}

	// Double resolve should return false
	if client.ResolvePending(reqID, testPayload) {
		t.Error("expected second ResolvePending to return false")
	}
}

func TestRegistry(t *testing.T) {
	registry := NewRegistry()
	if registry.ConnectedCount() != 0 {
		t.Errorf("expected 0 connected, got %d", registry.ConnectedCount())
	}

	client := NewClient("agent-001", nil)
	registry.Register(client)

	if !registry.IsConnected("agent-001") {
		t.Error("expected agent-001 to be connected")
	}
	if registry.ConnectedCount() != 1 {
		t.Errorf("expected 1 connected, got %d", registry.ConnectedCount())
	}
	if registry.GetClient("agent-001") != client {
		t.Error("expected GetClient to return registered client")
	}

	registry.Unregister("agent-001")
	if registry.IsConnected("agent-001") {
		t.Error("expected agent-001 to be disconnected after unregister")
	}
	if registry.ConnectedCount() != 0 {
		t.Errorf("expected 0 connected, got %d", registry.ConnectedCount())
	}
}
