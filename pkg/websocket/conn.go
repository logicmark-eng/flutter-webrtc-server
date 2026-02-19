package websocket

import (
	"errors"
	"net"
	"sync"
	"time"

	"github.com/chuckpreslar/emission"
	"github.com/flutter-webrtc/flutter-webrtc-server/pkg/logger"
	"github.com/gorilla/websocket"
)

const defaultPingInterval = 5 * time.Second
const defaultPongTimeout = 3 * defaultPingInterval

type WebSocketConn struct {
	emission.Emitter
	socket       *websocket.Conn
	mutex        *sync.Mutex
	closed       bool
	closeOnce    sync.Once
	pingInterval time.Duration
	pongTimeout  time.Duration
}

func NewWebSocketConn(socket *websocket.Conn, pingInterval, pongTimeout time.Duration) *WebSocketConn {
	if pingInterval <= 0 {
		pingInterval = defaultPingInterval
	}
	if pongTimeout <= 0 {
		pongTimeout = defaultPongTimeout
	}

	var conn WebSocketConn
	conn.Emitter = *emission.NewEmitter()
	conn.socket = socket
	conn.mutex = new(sync.Mutex)
	conn.closed = false
	conn.pingInterval = pingInterval
	conn.pongTimeout = pongTimeout
	conn.socket.SetCloseHandler(func(code int, text string) error {
		logger.Warnf("%s [%d]", text, code)
		conn.emitClose(code, text)
		return nil
	})
	// Reset read deadline on pong receipt (browser sends pong automatically)
	conn.socket.SetPongHandler(func(appData string) error {
		conn.socket.SetReadDeadline(time.Now().Add(conn.pongTimeout))
		return nil
	})
	return &conn
}

func (conn *WebSocketConn) ReadMessage() {
	in := make(chan []byte)
	stop := make(chan struct{})
	pingTicker := time.NewTicker(conn.pingInterval)
	defer pingTicker.Stop()

	var c = conn.socket
	// Set initial read deadline; subsequent resets happen via pong handler
	// or when any application message is received
	c.SetReadDeadline(time.Now().Add(conn.pongTimeout))
	go func() {
		for {
			_, message, err := c.ReadMessage()
			if err != nil {
				logger.Warnf("Got error: %v", err)
				if c, k := err.(*websocket.CloseError); k {
					conn.emitClose(c.Code, c.Text)
				} else if netErr, k := err.(net.Error); k && netErr.Timeout() {
					conn.emitClose(1006, "pong timeout")
				} else if c, k := err.(*net.OpError); k {
					conn.emitClose(1008, c.Error())
				}
				close(stop)
				break
			}
			// Any received message proves the client is alive — reset deadline.
			// This supports clients that respond with application-level keepalive
			// instead of (or in addition to) WebSocket pong frames.
			c.SetReadDeadline(time.Now().Add(conn.pongTimeout))
			in <- message
		}
	}()

	for {
		select {
		case _ = <-pingTicker.C:
			// Send WebSocket ping frame for connection liveness detection
			conn.mutex.Lock()
			pingErr := conn.socket.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(conn.pingInterval))
			conn.mutex.Unlock()
			if pingErr != nil {
				logger.Errorf("WebSocket ping failed: %v", pingErr)
				conn.emitClose(1006, "ping failed")
				conn.socket.Close()
				return
			}
			// Send application-level keepalive for clients that may not
			// support WebSocket ping/pong (e.g., some Python libraries,
			// custom embedded clients). Clients SHOULD respond with
			// {"type":"keepalive"} to keep the connection alive.
			if err := conn.Send(`{"type":"keepalive"}`); err != nil {
				logger.Errorf("Keepalive has failed")
				conn.emitClose(1006, "keepalive failed")
				conn.socket.Close()
				return
			}
		case message := <-in:
			{
				logger.Infof("Received data: %s", message)
				conn.Emit("message", []byte(message))
			}
		case <-stop:
			return
		}
	}
}

func (conn *WebSocketConn) emitClose(code int, text string) {
	conn.closeOnce.Do(func() {
		conn.mutex.Lock()
		conn.closed = true
		conn.mutex.Unlock()
		conn.Emit("close", code, text)
	})
}

/*
* Send |message| to the connection.
 */
func (conn *WebSocketConn) Send(message string) error {
	logger.Infof("Send data: %s", message)
	conn.mutex.Lock()
	defer conn.mutex.Unlock()
	if conn.closed {
		return errors.New("websocket: write closed")
	}
	return conn.socket.WriteMessage(websocket.TextMessage, []byte(message))
}

/*
* Close conn.
 */
func (conn *WebSocketConn) Close() {
	conn.mutex.Lock()
	defer conn.mutex.Unlock()
	if conn.closed == false {
		logger.Infof("Close ws conn now : ", conn)
		conn.socket.Close()
		conn.closed = true
	} else {
		logger.Warnf("Transport already closed :", conn)
	}
}
