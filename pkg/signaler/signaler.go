package signaler

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/flutter-webrtc/flutter-webrtc-server/pkg/logger"
	"github.com/flutter-webrtc/flutter-webrtc-server/pkg/turn"
	"github.com/flutter-webrtc/flutter-webrtc-server/pkg/util"
	"github.com/flutter-webrtc/flutter-webrtc-server/pkg/websocket"
)

const (
	sharedKey = `flutter-webrtc-turn-server-shared-key`
)

type TurnCredentials struct {
	Username string   `json:"username"`
	Password string   `json:"password"`
	TTL      int      `json:"ttl"`
	Uris     []string `json:"uris"`
}

// Peer .
type Peer struct {
	info PeerInfo
	conn *websocket.WebSocketConn
}

// Session info.
type Session struct {
	id   string
	from Peer
	to   Peer
}

type Method string

const (
	New       Method = "new"
	Bye       Method = "bye"
	Offer     Method = "offer"
	Answer    Method = "answer"
	Candidate Method = "candidate"
	Leave     Method = "leave"
	Keepalive Method = "keepalive"
)

type Request struct {
	Type Method      `json:"type"`
	Data interface{} `json:"data"`
}

type PeerInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	UserAgent string `json:"user_agent"`
}

type Negotiation struct {
	From      string `json:"from"`
	To        string `json:"to"`
	SessionID string `json:"session_id"`
}

type Byebye struct {
	SessionID string `json:"session_id"`
	From      string `json:"from"`
}

type Error struct {
	Request string `json:"request"`
	Reason  string `json:"reason"`
}

type Signaler struct {
	peers     map[string]Peer
	sessions  map[string]Session
	turn      *turn.TurnServer
	expresMap *util.ExpiredMap
	peerMutex sync.RWMutex
}

func NewSignaler(turn *turn.TurnServer) *Signaler {
	var signaler = &Signaler{
		peers:     make(map[string]Peer),
		sessions:  make(map[string]Session),
		turn:      turn,
		expresMap: util.NewExpiredMap(),
	}
	signaler.turn.AuthHandler = signaler.authHandler
	return signaler
}

func (s Signaler) authHandler(username string, realm string, srcAddr net.Addr) (string, bool) {
	// handle turn credential.
	if found, info := s.expresMap.Get(username); found {
		credential, ok := info.(TurnCredentials)
		if !ok {
			logger.Errorf("TURN auth: invalid credential type for username: %s", username)
			return "", false
		}
		logger.Infof("TURN auth: success for username=%s from=%s", username, srcAddr.String())
		return credential.Password, true
	}
	logger.Warnf("TURN auth: failed - username=%s not found (from=%s)", username, srcAddr.String())
	return "", false
}

// NotifyPeersUpdate .
func (s *Signaler) NotifyPeersUpdate(conn *websocket.WebSocketConn, peers map[string]Peer) {
	s.peerMutex.RLock()
	defer s.peerMutex.RUnlock()

	infos := []PeerInfo{}
	for _, peer := range peers {
		infos = append(infos, peer.info)
	}

	request := Request{
		Type: "peers",
		Data: infos,
	}

	for _, peer := range peers {
		s.Send(peer.conn, request)
	}
}

// HandleTurnServerCredentials .
// https://tools.ietf.org/html/draft-uberti-behave-turn-rest-00
func (s *Signaler) HandleTurnServerCredentials(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Access-Control-Allow-Origin", "*")

	params, err := url.ParseQuery(request.URL.RawQuery)
	if err != nil {
		http.Error(writer, "Invalid query parameters", http.StatusBadRequest)
		return
	}

	services, ok := params["service"]
	if !ok || len(services) == 0 {
		http.Error(writer, "Missing service parameter", http.StatusBadRequest)
		return
	}
	if services[0] != "turn" {
		http.Error(writer, "Invalid service parameter", http.StatusBadRequest)
		return
	}

	usernames, ok := params["username"]
	if !ok || len(usernames) == 0 {
		http.Error(writer, "Missing username parameter", http.StatusBadRequest)
		return
	}
	username := usernames[0]
	logger.Debugf("TURN credentials request: service=%s, username=%s", services[0], username)
	timestamp := time.Now().Unix()
	turnUsername := fmt.Sprintf("%d:%s", timestamp, username)
	hmac := hmac.New(sha1.New, []byte(sharedKey))
	hmac.Write([]byte(turnUsername))
	turnPassword := base64.RawStdEncoding.EncodeToString(hmac.Sum(nil))
	/*
		{
		     "username" : "12334939:mbzrxpgjys",
		     "password" : "adfsaflsjfldssia",
		     "ttl" : 86400,
		     "uris" : [
		       "turn:1.2.3.4:9991?transport=udp",
		       "turn:1.2.3.4:9992?transport=tcp",
		       "turns:1.2.3.4:443?transport=tcp"
			 ]
		}
		For client pc.
		var iceServer = {
			"username": response.username,
			"credential": response.password,
			"uris": response.uris
		};
		var config = {"iceServers": [iceServer]};
		var pc = new RTCPeerConnection(config);

	*/
	ttl := 86400
	hostUDP := fmt.Sprintf("%s:%d", s.turn.Config.PublicIP, s.turn.Config.Port)
	hostTCP := fmt.Sprintf("%s:%d", s.turn.Config.PublicIP, s.turn.Config.PortTCP)
	credential := TurnCredentials{
		Username: turnUsername,
		Password: turnPassword,
		TTL:      ttl,
		Uris: []string{
			"turn:" + hostUDP + "?transport=udp",
			"turn:" + hostTCP + "?transport=tcp",
		},
	}
	s.expresMap.Set(turnUsername, credential, int64(ttl))
	json.NewEncoder(writer).Encode(credential)
}

func (s *Signaler) Send(conn *websocket.WebSocketConn, m interface{}) error {
	data, err := json.Marshal(m)
	if err != nil {
		logger.Errorf(err.Error())
		return err
	}
	return conn.Send(string(data))
}

func (s *Signaler) HandleNewWebSocket(conn *websocket.WebSocketConn, request *http.Request) {
	logger.Infof("On Open %v", request)
	conn.On("message", func(message []byte) {
		logger.Infof("On message %v", string(message))
		var body json.RawMessage
		request := Request{
			Data: &body,
		}
		err := json.Unmarshal(message, &request)
		if err != nil {
			logger.Errorf("Unmarshal error %v", err)
			return
		}

		var data map[string]interface{}
		err = json.Unmarshal(body, &data)
		if err != nil {
			logger.Errorf("Unmarshal error %v", err)
			return
		}

		switch request.Type {
		case New:
			var info PeerInfo
			err := json.Unmarshal(body, &info)
			if err != nil {
				logger.Errorf("Unmarshal login error %v", err)
				return
			}
			s.peerMutex.Lock()
			s.peers[info.ID] = Peer{
				conn: conn,
				info: info,
			}
			s.peerMutex.Unlock()
			s.NotifyPeersUpdate(conn, s.peers)
			break
		case Leave:
		case Offer:
			fallthrough
		case Answer:
			fallthrough
		case Candidate:
			{
				var negotiation Negotiation
				err := json.Unmarshal(body, &negotiation)
				if err != nil {
					logger.Errorf("Unmarshal "+string(request.Type)+" got error %v", err)
					return
				}
				to := negotiation.To
				s.peerMutex.RLock()
				peer, ok := s.peers[to]
				s.peerMutex.RUnlock()
				if !ok {
					msg := Request{
						Type: "error",
						Data: Error{
							Request: string(request.Type),
							Reason:  "Peer [" + to + "] not found ",
						},
					}
					s.Send(conn, msg)
					return
				}
				s.Send(peer.conn, request)
			}
			break
		case Bye:
			var bye Byebye
			err := json.Unmarshal(body, &bye)
			if err != nil {
				logger.Errorf("Unmarshal bye got error %v", err)
				return
			}

			ids := strings.Split(bye.SessionID, "~")
			if len(ids) != 2 {
				msg := Request{
					Type: "error",
					Data: Error{
						Request: string(request.Type),
						Reason:  "Invalid session [" + bye.SessionID + "]",
					},
				}
				s.Send(conn, msg)
				return
			}

			sendBye := func(id string) {
				s.peerMutex.RLock()
				peer, ok := s.peers[id]
				s.peerMutex.RUnlock()

				if !ok {
					msg := Request{
						Type: "error",
						Data: Error{
							Request: string(request.Type),
							Reason:  "Peer [" + id + "] not found.",
						},
					}
					s.Send(conn, msg)
					return
				}
				bye := Request{
					Type: "bye",
					Data: map[string]interface{}{
						"to":         id,
						"session_id": bye.SessionID,
					},
				}
				s.Send(peer.conn, bye)
			}

			// send to aleg
			sendBye(ids[0])
			//send to bleg
			sendBye(ids[1])

		case Keepalive:
			s.Send(conn, request)
		default:
			logger.Warnf("Unkown request %v", request)
		}
	})

	conn.On("close", func(code int, text string) {
		logger.Infof("On Close %v", conn)

		// First, find the peer ID of the disconnecting peer
		s.peerMutex.RLock()
		var peerID string = ""
		for _, peer := range s.peers {
			if peer.conn == conn {
				peerID = peer.info.ID
				break
			}
		}
		s.peerMutex.RUnlock()

		if peerID == "" {
			logger.Warnf("Close event for unknown peer connection")
			return
		}

		logger.Infof("Peer %s disconnected", peerID)

		// Remove the peer from the map
		s.peerMutex.Lock()
		delete(s.peers, peerID)
		s.peerMutex.Unlock()

		// Notify other peers that this peer has left
		s.peerMutex.RLock()
		for _, peer := range s.peers {
			leave := Request{
				Type: "leave",
				Data: peerID,  // Send the ID of the peer that left
			}
			s.Send(peer.conn, leave)
		}
		s.peerMutex.RUnlock()

		// Notify all remaining peers of the updated peer list
		s.NotifyPeersUpdate(conn, s.peers)
	})
}
