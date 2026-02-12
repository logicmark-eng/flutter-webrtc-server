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

const pingPeriod = 5 * time.Second
const readDeadline = 3 * pingPeriod // Expect at least one message within 3 ping cycles

type WebSocketConn struct {
	emission.Emitter
	socket    *websocket.Conn
	mutex     *sync.Mutex
	closed    bool
	closeOnce sync.Once
}

func NewWebSocketConn(socket *websocket.Conn) *WebSocketConn {
	var conn WebSocketConn
	conn.Emitter = *emission.NewEmitter()
	conn.socket = socket
	conn.mutex = new(sync.Mutex)
	conn.closed = false
	conn.socket.SetCloseHandler(func(code int, text string) error {
		logger.Warnf("%s [%d]", text, code)
		conn.emitClose(code, text)
		return nil
	})
	return &conn
}

func (conn *WebSocketConn) ReadMessage() {
	in := make(chan []byte)
	stop := make(chan struct{})
	pingTicker := time.NewTicker(pingPeriod)

	var c = conn.socket
	c.SetReadDeadline(time.Now().Add(readDeadline))
	go func() {
		for {
			_, message, err := c.ReadMessage()
			if err != nil {
				logger.Warnf("Got error: %v", err)
				if c, k := err.(*websocket.CloseError); k {
					conn.emitClose(c.Code, c.Text)
				} else if netErr, k := err.(net.Error); k && netErr.Timeout() {
					conn.emitClose(1006, "read deadline exceeded")
				} else if c, k := err.(*net.OpError); k {
					conn.emitClose(1008, c.Error())
				}
				close(stop)
				break
			}
			c.SetReadDeadline(time.Now().Add(readDeadline))
			in <- message
		}
	}()

	for {
		select {
		case _ = <-pingTicker.C:
			logger.Infof("Send keepalive !!!")
			if err := conn.Send(`{"type":"keepalive"}`); err != nil {
				logger.Errorf("Keepalive has failed")
				pingTicker.Stop()
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
		conn.closed = true
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
