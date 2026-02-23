package websocket

import (
	"net/http"
	"strconv"
	"time"

	"github.com/flutter-webrtc/flutter-webrtc-server/pkg/logger"
	"github.com/gorilla/websocket"
)

type WebSocketServerConfig struct {
	Host           string
	Port           int
	CertFile       string
	KeyFile        string
	HTMLRoot       string
	WebSocketPath  string
	TurnServerPath string
	PingInterval   time.Duration
	PongTimeout    time.Duration
	HTTPMode       bool // run plain HTTP instead of HTTPS (useful for local dev/testing)
}

func DefaultConfig() WebSocketServerConfig {
	return WebSocketServerConfig{
		Host:           "0.0.0.0",
		Port:           8086,
		HTMLRoot:       "web",
		WebSocketPath:  "/ws",
		TurnServerPath: "/api/turn",
	}
}

type WebSocketServer struct {
	config           WebSocketServerConfig
	handleWebSocket  func(ws *WebSocketConn, request *http.Request)
	handleTurnServer func(writer http.ResponseWriter, request *http.Request)
	// Websocket upgrader
	upgrader websocket.Upgrader
}

func NewWebSocketServer(
	wsHandler func(ws *WebSocketConn, request *http.Request),
	turnServerHandler func(writer http.ResponseWriter, request *http.Request)) *WebSocketServer {
	var server = &WebSocketServer{
		handleWebSocket:  wsHandler,
		handleTurnServer: turnServerHandler,
		config:           DefaultConfig(),
	}
	server.upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
	return server
}

// Configure sets the server config without starting the server.
// Call this before Handler() when embedding the server in tests or custom HTTP stacks.
func (server *WebSocketServer) Configure(cfg WebSocketServerConfig) {
	server.config = cfg
}

// Handler returns an http.Handler with all routes registered on a private ServeMux.
// Call Configure (or Bind) before using this.
func (server *WebSocketServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(server.config.WebSocketPath, server.handleWebSocketRequest)
	mux.HandleFunc(server.config.TurnServerPath, server.handleTurnServerRequest)
	mux.Handle("/", http.FileServer(http.Dir(server.config.HTMLRoot)))
	return mux
}

func (server *WebSocketServer) handleWebSocketRequest(writer http.ResponseWriter, request *http.Request) {
	responseHeader := http.Header{}
	//responseHeader.Add("Sec-WebSocket-Protocol", "protoo")
	socket, err := server.upgrader.Upgrade(writer, request, responseHeader)
	if err != nil {
		logger.Panicf("%v", err)
	}
	wsTransport := NewWebSocketConn(socket, server.config.PingInterval, server.config.PongTimeout)
	server.handleWebSocket(wsTransport, request)
	wsTransport.ReadMessage()
}

func (server *WebSocketServer) handleTurnServerRequest(writer http.ResponseWriter, request *http.Request) {
	server.handleTurnServer(writer, request)
}

// Bind configures and starts the server (blocking). Use HTTPMode=true for plain HTTP.
func (server *WebSocketServer) Bind(cfg WebSocketServerConfig) {
	server.Configure(cfg)
	addr := cfg.Host + ":" + strconv.Itoa(cfg.Port)
	mux := server.Handler()
	logger.Infof("Flutter WebRTC Server listening on: %s:%d", cfg.Host, cfg.Port)
	if cfg.HTTPMode {
		panic(http.ListenAndServe(addr, mux))
	} else {
		panic(http.ListenAndServeTLS(addr, cfg.CertFile, cfg.KeyFile, mux))
	}
}
