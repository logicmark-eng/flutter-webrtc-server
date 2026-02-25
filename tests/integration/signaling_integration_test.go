package integration_test

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	gws "github.com/gorilla/websocket"

	"github.com/flutter-webrtc/flutter-webrtc-server/pkg/signaler"
	"github.com/flutter-webrtc/flutter-webrtc-server/pkg/websocket"
)

// ─── test server helpers ──────────────────────────────────────────────────────

// newTestServer creates an httptest.Server backed by the real signaling stack.
// pingInterval and pongTimeout control how quickly the server pings clients.
// Use large values (e.g. 30s / 60s) for tests that don't care about keepalive timing.
func newTestServer(t *testing.T, pingInterval, pongTimeout time.Duration) *httptest.Server {
	t.Helper()
	sig := signaler.NewSignaler(nil) // nil TURN — not needed for signaling tests
	wsServer := websocket.NewWebSocketServer(sig.HandleNewWebSocket, sig.HandleTurnServerCredentials)

	cfg := websocket.DefaultConfig()
	cfg.HTMLRoot = t.TempDir()
	cfg.PingInterval = pingInterval
	cfg.PongTimeout = pongTimeout
	wsServer.Configure(cfg)

	ts := httptest.NewServer(wsServer.Handler())
	t.Cleanup(ts.Close)
	return ts
}

// dial opens a WebSocket connection to /ws on the test server.
func dial(t *testing.T, ts *httptest.Server) *gws.Conn {
	t.Helper()
	u := url.URL{
		Scheme: "ws",
		Host:   strings.TrimPrefix(ts.URL, "http://"),
		Path:   "/ws",
	}
	dialer := gws.Dialer{HandshakeTimeout: 2 * time.Second}
	conn, _, err := dialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("dial %s: %v", u.String(), err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// sendJSON marshals v and sends it as a WebSocket text message.
func sendJSON(t *testing.T, conn *gws.Conn, v interface{}) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := conn.WriteMessage(gws.TextMessage, data); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// registerPeer sends the "new" registration message for a peer.
func registerPeer(t *testing.T, conn *gws.Conn, id, name string) {
	t.Helper()
	sendJSON(t, conn, map[string]interface{}{
		"type": "new",
		"data": map[string]interface{}{
			"id":         id,
			"name":       name,
			"user_agent": "integration-test",
		},
	})
}

// ─── msgChan ──────────────────────────────────────────────────────────────────

// msgChan drains WebSocket messages asynchronously into a buffered channel so
// tests can filter by type without missing interleaved keepalives or peer-list
// broadcasts.
type msgChan struct {
	ch chan map[string]interface{}
}

func newMsgChan(conn *gws.Conn) *msgChan {
	mc := &msgChan{ch: make(chan map[string]interface{}, 64)}
	go func() {
		defer close(mc.ch)
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg map[string]interface{}
			if err := json.Unmarshal(raw, &msg); err != nil {
				continue
			}
			mc.ch <- msg
		}
	}()
	return mc
}

// readType blocks until a message of the given type arrives (skipping others)
// or until the timeout elapses.
func (mc *msgChan) readType(t *testing.T, wantType string, timeout time.Duration) map[string]interface{} {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case msg, ok := <-mc.ch:
			if !ok {
				t.Fatalf("connection closed while waiting for type=%q", wantType)
			}
			if msg["type"] == wantType {
				return msg
			}
		case <-deadline:
			t.Fatalf("timed out after %v waiting for message type=%q", timeout, wantType)
			return nil
		}
	}
}

// assertNoType asserts that no message of the given type arrives within the wait
// duration. Other message types are silently discarded.
func (mc *msgChan) assertNoType(t *testing.T, badType string, wait time.Duration) {
	t.Helper()
	deadline := time.After(wait)
	for {
		select {
		case msg, ok := <-mc.ch:
			if !ok {
				return
			}
			if msg["type"] == badType {
				t.Fatalf("unexpectedly received message type=%q: %v", badType, msg)
			}
		case <-deadline:
			return
		}
	}
}

// ─── tests ────────────────────────────────────────────────────────────────────

// TestPeerRegistration verifies that a newly registered peer receives a "peers"
// broadcast containing only itself.
func TestPeerRegistration(t *testing.T) {
	ts := newTestServer(t, 30*time.Second, 60*time.Second)
	conn := dial(t, ts)
	mc := newMsgChan(conn)

	registerPeer(t, conn, "peer1", "Alice")

	msg := mc.readType(t, "peers", 2*time.Second)
	peers := msg["data"].([]interface{})
	if len(peers) != 1 {
		t.Fatalf("expected 1 peer, got %d", len(peers))
	}
	peer := peers[0].(map[string]interface{})
	if peer["id"] != "peer1" {
		t.Errorf("peer id: want peer1, got %v", peer["id"])
	}
}

// TestTwoPeersSeePeerList verifies that after two peers register, both receive
// a "peers" broadcast with 2 entries.
func TestTwoPeersSeePeerList(t *testing.T) {
	ts := newTestServer(t, 30*time.Second, 60*time.Second)
	connA := dial(t, ts)
	connB := dial(t, ts)
	mcA := newMsgChan(connA)
	mcB := newMsgChan(connB)

	registerPeer(t, connA, "a", "Alice")
	mcA.readType(t, "peers", 2*time.Second) // 1-peer list

	registerPeer(t, connB, "b", "Bob")

	// Both should now receive a 2-peer list
	msgA := mcA.readType(t, "peers", 2*time.Second)
	msgB := mcB.readType(t, "peers", 2*time.Second)

	for _, msg := range []map[string]interface{}{msgA, msgB} {
		peers := msg["data"].([]interface{})
		if len(peers) != 2 {
			t.Errorf("expected 2 peers, got %d", len(peers))
		}
	}
}

// TestDuplicatePeerID verifies that when a peer registers with an ID already in
// use, the server closes the old connection.
func TestDuplicatePeerID(t *testing.T) {
	ts := newTestServer(t, 30*time.Second, 60*time.Second)

	connA1 := dial(t, ts)
	mcA1 := newMsgChan(connA1)
	registerPeer(t, connA1, "dup", "First")
	mcA1.readType(t, "peers", 2*time.Second) // consume initial peers list

	connA2 := dial(t, ts)
	mcA2 := newMsgChan(connA2)
	registerPeer(t, connA2, "dup", "Second")
	mcA2.readType(t, "peers", 2*time.Second) // new peer gets list

	// The old connection should be closed by the server within 2 seconds.
	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-mcA1.ch:
			if !ok {
				return // channel closed — old connection was closed as expected
			}
		case <-deadline:
			t.Fatal("old connection was not closed after duplicate registration")
		}
	}
}

// TestOfferRouting verifies that an offer is routed only to the target peer (B),
// not echoed back to the sender (A).
func TestOfferRouting(t *testing.T) {
	ts := newTestServer(t, 30*time.Second, 60*time.Second)
	connA, connB, mcA, mcB := setupTwoPeers(t, ts, "a", "b")

	sendJSON(t, connA, map[string]interface{}{
		"type": "offer",
		"data": map[string]interface{}{
			"from":       "a",
			"to":         "b",
			"session_id": "a~b",
			"sdp":        "v=0\r\no=- 0 0 IN IP4 127.0.0.1\r\n",
		},
	})

	msg := mcB.readType(t, "offer", 2*time.Second)
	data := msg["data"].(map[string]interface{})
	if data["from"] != "a" {
		t.Errorf("offer.from: want a, got %v", data["from"])
	}
	mcA.assertNoType(t, "offer", 300*time.Millisecond)

	_ = connB // suppress unused warning
}

// TestAnswerRouting verifies that an answer is routed only to the target peer (A),
// not echoed back to the sender (B).
func TestAnswerRouting(t *testing.T) {
	ts := newTestServer(t, 30*time.Second, 60*time.Second)
	connA, connB, mcA, mcB := setupTwoPeers(t, ts, "a", "b")

	sendJSON(t, connB, map[string]interface{}{
		"type": "answer",
		"data": map[string]interface{}{
			"from":       "b",
			"to":         "a",
			"session_id": "a~b",
			"sdp":        "v=0\r\no=- 0 0 IN IP4 127.0.0.1\r\n",
		},
	})

	msg := mcA.readType(t, "answer", 2*time.Second)
	data := msg["data"].(map[string]interface{})
	if data["from"] != "b" {
		t.Errorf("answer.from: want b, got %v", data["from"])
	}
	mcB.assertNoType(t, "answer", 300*time.Millisecond)

	_ = connA
}

// TestCandidateRouting verifies that an ICE candidate is routed from A to B only.
func TestCandidateRouting(t *testing.T) {
	ts := newTestServer(t, 30*time.Second, 60*time.Second)
	connA, connB, mcA, mcB := setupTwoPeers(t, ts, "a", "b")

	sendJSON(t, connA, map[string]interface{}{
		"type": "candidate",
		"data": map[string]interface{}{
			"from":       "a",
			"to":         "b",
			"session_id": "a~b",
			"candidate":  "candidate:1 1 UDP 2130706431 192.0.2.1 54321 typ host",
		},
	})

	msg := mcB.readType(t, "candidate", 2*time.Second)
	data := msg["data"].(map[string]interface{})
	if data["from"] != "a" {
		t.Errorf("candidate.from: want a, got %v", data["from"])
	}
	mcA.assertNoType(t, "candidate", 300*time.Millisecond)

	_ = connB
}

// TestPingRouting verifies that the ping signal is routed from the sender (A) to
// the target (B) and is NOT echoed back to A.
func TestPingRouting(t *testing.T) {
	ts := newTestServer(t, 30*time.Second, 60*time.Second)
	connA, connB, mcA, mcB := setupTwoPeers(t, ts, "a", "b")

	sendJSON(t, connA, map[string]interface{}{
		"type": "ping",
		"data": map[string]interface{}{
			"from": "a",
			"to":   "b",
		},
	})

	msg := mcB.readType(t, "ping", 2*time.Second)
	data := msg["data"].(map[string]interface{})
	if data["from"] != "a" {
		t.Errorf("ping.from: want a, got %v", data["from"])
	}
	if data["to"] != "b" {
		t.Errorf("ping.to: want b, got %v", data["to"])
	}

	// Sender must NOT receive its own ping
	mcA.assertNoType(t, "ping", 300*time.Millisecond)

	_ = connA
	_ = connB
}

// TestPongRouting verifies the full ping→pong round-trip:
// A sends ping to B → B sends pong to A → A receives pong, B does not.
func TestPongRouting(t *testing.T) {
	ts := newTestServer(t, 30*time.Second, 60*time.Second)
	connA, connB, mcA, mcB := setupTwoPeers(t, ts, "a", "b")

	// A sends ping to B
	sendJSON(t, connA, map[string]interface{}{
		"type": "ping",
		"data": map[string]interface{}{"from": "a", "to": "b"},
	})

	// B receives the ping and replies with a pong (swapping from/to)
	mcB.readType(t, "ping", 2*time.Second)
	sendJSON(t, connB, map[string]interface{}{
		"type": "pong",
		"data": map[string]interface{}{"from": "b", "to": "a"},
	})

	// A receives the pong
	msg := mcA.readType(t, "pong", 2*time.Second)
	data := msg["data"].(map[string]interface{})
	if data["from"] != "b" {
		t.Errorf("pong.from: want b, got %v", data["from"])
	}
	if data["to"] != "a" {
		t.Errorf("pong.to: want a, got %v", data["to"])
	}

	// B must NOT receive its own pong echoed back
	mcB.assertNoType(t, "pong", 300*time.Millisecond)

	_ = connA
	_ = connB
}

// TestByeRouting verifies that a "bye" message is routed only to the remote peer
// and is NOT echoed back to the sender.
func TestByeRouting(t *testing.T) {
	ts := newTestServer(t, 30*time.Second, 60*time.Second)
	connA, connB, mcA, mcB := setupTwoPeers(t, ts, "a", "b")

	sendJSON(t, connA, map[string]interface{}{
		"type": "bye",
		"data": map[string]interface{}{
			"session_id": "a~b",
			"from":       "a",
		},
	})

	msg := mcB.readType(t, "bye", 2*time.Second)
	data := msg["data"].(map[string]interface{})
	if data["from"] != "a" {
		t.Errorf("bye.from: want a, got %v", data["from"])
	}

	// Sender must NOT receive bye echoed back
	mcA.assertNoType(t, "bye", 300*time.Millisecond)

	_ = connA
	_ = connB
}

// TestDisconnectLeaveNotification verifies that when a peer closes its connection
// unexpectedly, all remaining peers receive a "leave" message with the departed
// peer's ID.
func TestDisconnectLeaveNotification(t *testing.T) {
	ts := newTestServer(t, 30*time.Second, 60*time.Second)
	connA, _, _, mcB := setupTwoPeers(t, ts, "a", "b")

	// Close A's connection abruptly
	connA.Close()

	msg := mcB.readType(t, "leave", 2*time.Second)
	data := msg["data"].(map[string]interface{})
	if data["id"] != "a" {
		t.Errorf("leave.id: want a, got %v", data["id"])
	}
}

// TestKeepaliveHandling verifies that when a client responds to the server's
// keepalive, the connection stays alive. Uses short ping intervals to keep the
// test fast.
func TestKeepaliveHandling(t *testing.T) {
	ts := newTestServer(t, 200*time.Millisecond, 1*time.Second)
	conn := dial(t, ts)
	mc := newMsgChan(conn)

	registerPeer(t, conn, "k", "KeepAliveClient")
	mc.readType(t, "peers", 2*time.Second)

	// Wait for the server to send a keepalive
	mc.readType(t, "keepalive", 1*time.Second)

	// Respond — this resets the server's read deadline
	sendJSON(t, conn, map[string]interface{}{"type": "keepalive"})

	// Verify the connection is still alive by sending a ping to ourselves.
	// The server finds peer "k" and routes the ping back to the same connection.
	sendJSON(t, conn, map[string]interface{}{
		"type": "ping",
		"data": map[string]interface{}{"from": "k", "to": "k"},
	})
	mc.readType(t, "ping", 1*time.Second) // ping echoed back → connection alive
}

// TestPeerNotFoundError verifies that routing a message to a non-existent peer
// returns an error response to the sender.
func TestPeerNotFoundError(t *testing.T) {
	ts := newTestServer(t, 30*time.Second, 60*time.Second)
	conn := dial(t, ts)
	mc := newMsgChan(conn)

	registerPeer(t, conn, "a", "Alice")
	mc.readType(t, "peers", 2*time.Second)

	sendJSON(t, conn, map[string]interface{}{
		"type": "offer",
		"data": map[string]interface{}{
			"from":       "a",
			"to":         "ghost",
			"session_id": "a~ghost",
		},
	})

	msg := mc.readType(t, "error", 2*time.Second)
	data := msg["data"].(map[string]interface{})
	if data["reason"] == nil || data["reason"] == "" {
		t.Errorf("error message missing reason: %v", msg)
	}
}

// TestMultiplePeers registers three peers, verifies the full peer list is
// broadcast to all, then has one peer leave and checks that the remaining two
// receive the correct "leave" and updated "peers" messages.
func TestMultiplePeers(t *testing.T) {
	ts := newTestServer(t, 30*time.Second, 60*time.Second)
	connA := dial(t, ts)
	connB := dial(t, ts)
	connC := dial(t, ts)
	mcA := newMsgChan(connA)
	mcB := newMsgChan(connB)
	mcC := newMsgChan(connC)

	registerPeer(t, connA, "a", "Alice")
	mcA.readType(t, "peers", 2*time.Second) // 1-peer list

	registerPeer(t, connB, "b", "Bob")
	mcA.readType(t, "peers", 2*time.Second) // 2-peer list
	mcB.readType(t, "peers", 2*time.Second)

	registerPeer(t, connC, "c", "Carol")
	msgA := mcA.readType(t, "peers", 2*time.Second) // 3-peer list
	mcB.readType(t, "peers", 2*time.Second)
	mcC.readType(t, "peers", 2*time.Second)

	if n := len(msgA["data"].([]interface{})); n != 3 {
		t.Fatalf("expected 3 peers after all register, got %d", n)
	}

	// C disconnects
	connC.Close()

	// A and B must receive "leave" for C
	leaveA := mcA.readType(t, "leave", 2*time.Second)
	if leaveA["data"].(map[string]interface{})["id"] != "c" {
		t.Errorf("leave.id: want c, got %v", leaveA["data"])
	}
	mcB.readType(t, "leave", 2*time.Second)

	// And then get an updated peers list with only 2 peers
	updatedA := mcA.readType(t, "peers", 2*time.Second)
	if n := len(updatedA["data"].([]interface{})); n != 2 {
		t.Fatalf("expected 2 peers after C leaves, got %d", n)
	}
}

// ─── shared setup helpers ─────────────────────────────────────────────────────

// setupTwoPeers registers two peers and drains the initial "peers" broadcasts
// so that subsequent test reads only see the messages they care about.
func setupTwoPeers(
	t *testing.T,
	ts *httptest.Server,
	idA, idB string,
) (*gws.Conn, *gws.Conn, *msgChan, *msgChan) {
	t.Helper()
	connA := dial(t, ts)
	connB := dial(t, ts)
	mcA := newMsgChan(connA)
	mcB := newMsgChan(connB)

	registerPeer(t, connA, idA, idA)
	mcA.readType(t, "peers", 2*time.Second) // 1-peer list

	registerPeer(t, connB, idB, idB)
	mcA.readType(t, "peers", 2*time.Second) // 2-peer list (A sees B join)
	mcB.readType(t, "peers", 2*time.Second) // 2-peer list (B's first list)

	return connA, connB, mcA, mcB
}
