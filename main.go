package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media/samplebuilder"
)

// SignalingMessage represents WebSocket signaling data
type SignalingMessage struct {
	Type    string                     `json:"type"`
	SDP     *webrtc.SessionDescription `json:"sdp,omitempty"`
	ICE     *webrtc.ICECandidateInit  `json:"ice,omitempty"`
	Error   string                     `json:"error,omitempty"`
}

// WebRTCManager handles peer connection and tracks
type WebRTCManager struct {
	peerConnection *webrtc.PeerConnection
	videoTrack     *webrtc.TrackLocalStaticSample
	audioTrack     *webrtc.TrackLocalStaticSample
	wsConn         *websocket.Conn
	mu             sync.Mutex
	udpListener    *net.UDPConn
	stopChan       chan struct{}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins in development
	},
}

func main() {
	// Serve static files
	fs := http.FileServer(http.Dir("static"))
	http.Handle("/", fs)

	// WebSocket endpoint
	http.HandleFunc("/ws", handleWebSocket)

	port := ":8080"
	log.Printf("Server starting on %s", port)
	log.Fatal(http.ListenAndServe(port, nil))
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	handleWebRTCConnection(conn)
}

func handleWebRTCConnection(wsConn *websocket.Conn) {
	manager, err := newWebRTCManager(wsConn)
	if err != nil {
		log.Printf("Failed to create WebRTC manager: %v", err)
		return
	}
	defer manager.close()

	// Start the media handling on a single port
	go manager.handleMedia()

	// Handle incoming WebSocket messages
	for {
		var msg SignalingMessage
		if err := wsConn.ReadJSON(&msg); err != nil {
			log.Printf("WebSocket read error: %v", err)
			break
		}

		if err := manager.handleSignalingMessage(msg); err != nil {
			log.Printf("Failed to handle signaling message: %v", err)
			continue
		}
	}
}

func newWebRTCManager(wsConn *websocket.Conn) (*WebRTCManager, error) {
	// Create MediaEngine
	mediaEngine := &webrtc.MediaEngine{}

	// Register default codecs
	if err := mediaEngine.RegisterDefaultCodecs(); err != nil {
		return nil, fmt.Errorf("failed to register default codecs: %v", err)
	}

	// Create API with MediaEngine
	api := webrtc.NewAPI(webrtc.WithMediaEngine(mediaEngine))

	// Create PeerConnection with STUN server
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}

	peerConnection, err := api.NewPeerConnection(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create peer connection: %v", err)
	}

	manager := &WebRTCManager{
		peerConnection: peerConnection,
		wsConn:        wsConn,
		stopChan:      make(chan struct{}),
	}

	// Create video track
	videoTrack, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8},
		"video",
		"pion",
	)
	if err != nil {
		peerConnection.Close()
		return nil, fmt.Errorf("failed to create video track: %v", err)
	}
	manager.videoTrack = videoTrack

	// Create audio track
	audioTrack, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus},
		"audio",
		"pion",
	)
	if err != nil {
		peerConnection.Close()
		return nil, fmt.Errorf("failed to create audio track: %v", err)
	}
	manager.audioTrack = audioTrack

	// Add tracks to peer connection
	if _, err = peerConnection.AddTrack(videoTrack); err != nil {
		peerConnection.Close()
		return nil, fmt.Errorf("failed to add video track: %v", err)
	}

	if _, err = peerConnection.AddTrack(audioTrack); err != nil {
		peerConnection.Close()
		return nil, fmt.Errorf("failed to add audio track: %v", err)
	}

	// Set up ICE candidate handling
	peerConnection.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil {
			return
		}

		candidateInit := candidate.ToJSON()
		msg := SignalingMessage{
			Type: "ice",
			ICE:  &candidateInit,
		}

		manager.mu.Lock()
		defer manager.mu.Unlock()

		if err := wsConn.WriteJSON(msg); err != nil {
			log.Printf("Failed to send ICE candidate: %v", err)
		}
	})

	// Set up connection state handling
	peerConnection.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
		log.Printf("ICE Connection State has changed: %s\n", connectionState.String())
	})

	return manager, nil
}

func (m *WebRTCManager) handleSignalingMessage(msg SignalingMessage) error {
	switch msg.Type {
	case "offer":
		return m.handleOffer(msg.SDP)
	case "ice":
		return m.handleICECandidate(msg.ICE)
	default:
		return fmt.Errorf("unknown message type: %s", msg.Type)
	}
}

func (m *WebRTCManager) handleOffer(offer *webrtc.SessionDescription) error {
	// Set remote description first
	if err := m.peerConnection.SetRemoteDescription(*offer); err != nil {
		return fmt.Errorf("failed to set remote description: %v", err)
	}

	// Create answer
	answer, err := m.peerConnection.CreateAnswer(nil)
	if err != nil {
		return fmt.Errorf("failed to create answer: %v", err)
	}

	// Set local description
	if err = m.peerConnection.SetLocalDescription(answer); err != nil {
		return fmt.Errorf("failed to set local description: %v", err)
	}

	// Wait for ICE gathering to complete
	gatherComplete := webrtc.GatheringCompletePromise(m.peerConnection)
	<-gatherComplete

	// Send answer with gathered ICE candidates
	msg := SignalingMessage{
		Type: "answer",
		SDP:  m.peerConnection.LocalDescription(),
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.wsConn.WriteJSON(msg); err != nil {
		return fmt.Errorf("failed to send answer: %v", err)
	}

	// Remove the separate rtpToTrack calls since we're now using handleMedia
	return nil
}

func (m *WebRTCManager) handleICECandidate(candidate *webrtc.ICECandidateInit) error {
	if candidate == nil {
		return nil
	}

	return m.peerConnection.AddICECandidate(*candidate)
}

func (m *WebRTCManager) handleMedia() {
	var err error
	m.udpListener, err = net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 5004})
	if err != nil {
		log.Printf("Failed to listen on UDP port: %v", err)
		return
	}
	defer m.udpListener.Close()

	videoBuffer := samplebuilder.New(10, &codecs.VP8Packet{}, 90000)
	audioBuffer := samplebuilder.New(10, &codecs.OpusPacket{}, 48000)

	// Remove the readChan declaration
	go func() {
		for {
			select {
			case <-m.stopChan:
				return
			default:
				inboundRTPPacket := make([]byte, 1500)
				packet := &rtp.Packet{}

				// Set read deadline
				m.udpListener.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
				n, _, err := m.udpListener.ReadFrom(inboundRTPPacket)
				if err != nil {
					if !os.IsTimeout(err) {
						log.Printf("Error during read: %v", err)
					}
					continue
				}

				if err = packet.Unmarshal(inboundRTPPacket[:n]); err != nil {
					log.Printf("Error unmarshaling RTP packet: %v", err)
					continue
				}

				switch packet.PayloadType {
				case 96: // VP8 Video
					videoBuffer.Push(packet)
					for {
						sample := videoBuffer.Pop()
						if sample == nil {
							break
						}
						if err := m.videoTrack.WriteSample(*sample); err != nil {
							log.Printf("Error writing video sample: %v", err)
						}
					}
				case 111: // Opus Audio
					audioBuffer.Push(packet)
					for {
						sample := audioBuffer.Pop()
						if sample == nil {
							break
						}
						if err := m.audioTrack.WriteSample(*sample); err != nil {
							log.Printf("Error writing audio sample: %v", err)
						}
					}
				}
			}
		}
	}()

	<-m.stopChan
}

func (m *WebRTCManager) close() {
	// Signal to stop the media handling goroutine
	close(m.stopChan)

	// Close the UDP listener if it exists
	if m.udpListener != nil {
		m.udpListener.Close()
	}

	// Close the peer connection
	if err := m.peerConnection.Close(); err != nil {
		log.Printf("Error closing peer connection: %v", err)
	}
}


