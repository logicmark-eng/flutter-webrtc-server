package turn

import (
	"net"
	"strconv"

	"github.com/flutter-webrtc/flutter-webrtc-server/pkg/logger"
	"github.com/pion/turn/v2"
)

type TurnServerConfig struct {
	PublicIP string
	Port     int
	PortTCP  int
	Realm    string
}

func DefaultConfig() TurnServerConfig {
	return TurnServerConfig{
		PublicIP: "127.0.0.1",
		Port:     19302,
		PortTCP:  19303,
		Realm:    "flutter-webrtc",
	}
}

/*
if key, ok := usersMap[username]; ok {
				return key, true
			}
			return nil, false
*/

type TurnServer struct {
	udpListener net.PacketConn
	tcpListener net.Listener
	turnServer  *turn.Server
	Config      TurnServerConfig
	AuthHandler func(username string, realm string, srcAddr net.Addr) (string, bool)
}

func resolvePublicIP(publicIP string) net.IP {
	ip := net.ParseIP(publicIP)
	if ip != nil {
		return ip
	}
	// Not a numeric IP — try resolving as domain name
	ips, err := net.LookupIP(publicIP)
	if err != nil || len(ips) == 0 {
		logger.Panicf("Cannot resolve public_ip '%s': %v", publicIP, err)
	}
	// Prefer IPv4
	for _, resolved := range ips {
		if resolved.To4() != nil {
			logger.Infof("Resolved public_ip '%s' to %s", publicIP, resolved.String())
			return resolved
		}
	}
	logger.Infof("Resolved public_ip '%s' to %s", publicIP, ips[0].String())
	return ips[0]
}

func NewTurnServer(config TurnServerConfig) *TurnServer {
	server := &TurnServer{
		Config:      config,
		AuthHandler: nil,
	}
	if len(config.PublicIP) == 0 {
		logger.Panicf("'public-ip' is required")
	}

	relayIP := resolvePublicIP(config.PublicIP)

	// Create UDP listener
	udpListener, err := net.ListenPacket("udp4", "0.0.0.0:"+strconv.Itoa(config.Port))
	if err != nil {
		logger.Panicf("Failed to create TURN server UDP listener: %s", err)
	}
	server.udpListener = udpListener
	logger.Infof("TURN server UDP listener started on port %d", config.Port)

	// Create TCP listener
	tcpListener, err := net.Listen("tcp4", "0.0.0.0:"+strconv.Itoa(config.PortTCP))
	if err != nil {
		udpListener.Close() // Clean up UDP listener on failure
		logger.Panicf("Failed to create TURN server TCP listener: %s", err)
	}
	server.tcpListener = tcpListener
	logger.Infof("TURN server TCP listener started on port %d", config.PortTCP)

	turnServer, err := turn.NewServer(turn.ServerConfig{
		Realm:       config.Realm,
		AuthHandler: server.HandleAuthenticate,
		PacketConnConfigs: []turn.PacketConnConfig{
			{
				PacketConn: udpListener,
				RelayAddressGenerator: &turn.RelayAddressGeneratorStatic{
					RelayAddress: relayIP,
					Address:      "0.0.0.0",
				},
			},
		},
		ListenerConfigs: []turn.ListenerConfig{
			{
				Listener: tcpListener,
				RelayAddressGenerator: &turn.RelayAddressGeneratorStatic{
					RelayAddress: relayIP,
					Address:      "0.0.0.0",
				},
			},
		},
	})
	if err != nil {
		udpListener.Close() // Clean up listeners on failure
		tcpListener.Close()
		logger.Panicf("%v", err)
	}
	server.turnServer = turnServer
	return server
}

func (s *TurnServer) HandleAuthenticate(username string, realm string, srcAddr net.Addr) ([]byte, bool) {
	if s.AuthHandler != nil {
		if password, ok := s.AuthHandler(username, realm, srcAddr); ok {
			return turn.GenerateAuthKey(username, realm, password), true
		}
	}
	return nil, false
}

func (s *TurnServer) Close() error {
	return s.turnServer.Close()
}
