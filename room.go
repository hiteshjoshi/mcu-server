package main

import (
	"fmt"
	"math/rand"
	"net/http"

	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v2"
)

// Peer config
var peerConnectionConfig = webrtc.Configuration{
	ICEServers: []webrtc.ICEServer{
		{
			URLs: []string{"stun:stun.l.google.com:19302"},
		},
	},
	SDPSemantics: webrtc.SDPSemanticsUnifiedPlanWithFallback,
}

var (
	// Media engine
	m webrtc.MediaEngine

	// API object
	api *webrtc.API

	// Publisher Peer
	pubCount int32
	//user.conn *webrtc.PeerConnection

	// Websocket upgrader
	upgrader = websocket.Upgrader{}

	// Broadcast channels
	broadcastHub = newHub()

	userPipelines []*User
)

const (
	rtcpPLIInterval = time.Second * 3
)

type Client struct {
	id        string
	conn      *webrtc.PeerConnection
	opustrack *webrtc.RTPSender
}

func room(w http.ResponseWriter, r *http.Request) {

	// Websocket client
	c, err := upgrader.Upgrade(w, r, nil)

	checkError(err)

	defer func() {
		checkError(c.Close())
	}()

	// Read sdp from websocket
	mt, msg, err := c.ReadMessage()
	checkError(err)

	atomic.AddInt32(&pubCount, 1)
	userID := fmt.Sprintf("%d", atomic.LoadInt32(&pubCount))

	user := &User{id: userID}
	// Create a new RTCPeerConnection
	user.conn, err = api.NewPeerConnection(peerConnectionConfig)
	checkError(err)

	_, err = user.conn.AddTransceiver(webrtc.RTPCodecTypeAudio)
	checkError(err)

	user.conn.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
		fmt.Printf("ICE Connection State has changed: %s\n", connectionState.String())
	})

	user.conn.OnConnectionStateChange(func(p webrtc.PeerConnectionState) {
		fmt.Printf("Peer Connection State has changed: %s\n", p.String())
	})
	user.conn.OnSignalingStateChange(func(s webrtc.SignalingState) {
		fmt.Printf("Signal State  has changed: %s\n", s.String())
	})

	opustrack, err := user.conn.NewTrack(webrtc.DefaultPayloadTypeOpus, rand.Uint32(), "audio", "pion-server1")
	if err != nil {
		checkError(err)
	}
	// Add local audio track
	user.outTrack, err = user.conn.AddTrack(opustrack)
	checkError(err)

	user.conn.OnTrack(user.gotTrack)
	// Set the remote SessionDescription
	checkError(user.conn.SetRemoteDescription(
		webrtc.SessionDescription{
			SDP:  string(msg),
			Type: webrtc.SDPTypeOffer,
		}))

	// Create answer
	answer, err := user.conn.CreateAnswer(nil)
	checkError(err)

	// Sets the LocalDescription, and starts our UDP listeners
	checkError(user.conn.SetLocalDescription(answer))

	// Send server sdp to publisher
	checkError(c.WriteMessage(mt, []byte(answer.SDP)))

	// Register incoming channel
	user.conn.OnDataChannel(func(d *webrtc.DataChannel) {
		d.OnMessage(func(msg webrtc.DataChannelMessage) {
			// Broadcast the data to subSenders
			broadcastHub.broadcastChannel <- msg.Data
		})
	})

}
