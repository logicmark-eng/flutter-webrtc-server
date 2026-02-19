package main

import (
	"os"
	"time"

	"github.com/flutter-webrtc/flutter-webrtc-server/pkg/logger"
	"github.com/flutter-webrtc/flutter-webrtc-server/pkg/signaler"
	"github.com/flutter-webrtc/flutter-webrtc-server/pkg/turn"
	"github.com/flutter-webrtc/flutter-webrtc-server/pkg/websocket"
	"gopkg.in/ini.v1"
)

func main() {

	cfg, err := ini.Load("configs/config.ini")
	if err != nil {
		logger.Errorf("Fail to read file: %v", err)
		os.Exit(1)
	}

	publicIP := cfg.Section("turn").Key("public_ip").String()
	stunPort, err := cfg.Section("turn").Key("port").Int()
	if err != nil {
		stunPort = 3478
	}
	stunPortTCP, err := cfg.Section("turn").Key("port_tcp").Int()
	if err != nil {
		stunPortTCP = 3479
	}
	realm := cfg.Section("turn").Key("realm").String()

	turnConfig := turn.DefaultConfig()
	turnConfig.PublicIP = publicIP
	turnConfig.Port = stunPort
	turnConfig.PortTCP = stunPortTCP
	turnConfig.Realm = realm
	turn := turn.NewTurnServer(turnConfig)

	signaler := signaler.NewSignaler(turn)
	wsServer := websocket.NewWebSocketServer(signaler.HandleNewWebSocket, signaler.HandleTurnServerCredentials)

	sslCert := cfg.Section("general").Key("cert").String()
	sslKey := cfg.Section("general").Key("key").String()
	bindAddress := cfg.Section("general").Key("bind").String()

	port, err := cfg.Section("general").Key("port").Int()
	if err != nil {
		port = 8086
	}

	htmlRoot := cfg.Section("general").Key("html_root").String()

	pingInterval, err := cfg.Section("websocket").Key("ping_interval").Int()
	if err != nil {
		pingInterval = 5
	}
	pongTimeout, err := cfg.Section("websocket").Key("pong_timeout").Int()
	if err != nil {
		pongTimeout = 15
	}

	config := websocket.DefaultConfig()
	config.Host = bindAddress
	config.Port = port
	config.CertFile = sslCert
	config.KeyFile = sslKey
	config.HTMLRoot = htmlRoot
	config.PingInterval = time.Duration(pingInterval) * time.Second
	config.PongTimeout = time.Duration(pongTimeout) * time.Second

	wsServer.Bind(config)
}
