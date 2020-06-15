package main

import (
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"sync"

	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v2"
	"github.com/pion/webrtc/v2/pkg/media"

	"github.com/hiteshjoshi/webrtc/gst"
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
	pubCount    int32
	pubReceiver *webrtc.PeerConnection

	// Local track
	videoTrack     *webrtc.Track
	audioTrack     *gst.Pipeline
	videoTrackLock = sync.RWMutex{}
	audioTrackLock = sync.RWMutex{}

	// Websocket upgrader
	upgrader = websocket.Upgrader{}

	// Broadcast channels
	broadcastHub = newHub()
)

const (
	rtcpPLIInterval = time.Second * 3
)

func initGst() {
	audioTrack, _ = gst.PipelineNew("myapp")
	//sink, _ := gst.ElementFactoryMake("appsink", "asinks")
	//sink.SetObject("name", "sink")

	audioMixer, _ := gst.ElementFactoryMake("audiomixer", "audiomixer")
	audioMixer.SetObject("name", "mix")

	mixerConvert, _ := gst.ElementFactoryMake("audioconvert", "mixerconvert")
	//audioresample, _ := gst.ElementFactoryMake("audioresample", "audioresample")

	opusenc, _ := gst.ElementFactoryMake("opusenc", "opusenc")

	oggmux, _ := gst.ElementFactoryMake("oggmux", "oggmux")

	appsink, _ := gst.ElementFactoryMake("filesink", "appsink")
	appsink.SetObject("location", "file.ogg")

	audioTrack.AddMany(audioMixer, appsink, mixerConvert, oggmux, opusenc)

	log.Println(audioMixer.Link(mixerConvert))
	log.Println(mixerConvert.Link(opusenc))

	//log.Printf("opusenc: %t", audioresample.Link(opusenc))
	log.Println(opusenc.Link(oggmux))
	log.Println(oggmux.Link(appsink))

	log.Println(audioTrack.Name())

	//log.Printf("opusenc: %d", audioresample.GetStaticPad("src").Link(opusenc.GetStaticPad("sink")))

	audioTrack.SetState(gst.StateReady)
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

	//atomic.AddInt32(&pubCount, 1)

	// Create a new RTCPeerConnection
	pubReceiver, err = api.NewPeerConnection(peerConnectionConfig)
	checkError(err)

	_, err = pubReceiver.AddTransceiver(webrtc.RTPCodecTypeAudio)
	checkError(err)

	pubReceiver.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
		fmt.Printf("ICE Connection State has changed: %s\n", connectionState.String())
	})

	pubReceiver.OnConnectionStateChange(func(p webrtc.PeerConnectionState) {
		fmt.Printf("Peer Connection State has changed: %s\n", p.String())
	})
	pubReceiver.OnSignalingStateChange(func(s webrtc.SignalingState) {
		fmt.Printf("Signal State  has changed: %s\n", s.String())
	})

	//_, err = pubReceiver.AddTransceiver(webrtc.RTPCodecTypeVideo)
	//checkError(err)

	pubReceiver.OnTrack(func(remoteTrack *webrtc.Track, receiver *webrtc.RTPReceiver) {
		if remoteTrack.PayloadType() == webrtc.DefaultPayloadTypeVP8 || remoteTrack.PayloadType() == webrtc.DefaultPayloadTypeVP9 || remoteTrack.PayloadType() == webrtc.DefaultPayloadTypeH264 {
			return

			// Create a local video track, all our SFU clients will be fed via this track
			var err error
			videoTrackLock.Lock()
			videoTrack, err = pubReceiver.NewTrack(remoteTrack.PayloadType(), remoteTrack.SSRC(), "video", "pion")
			videoTrackLock.Unlock()
			checkError(err)

			// Send a PLI on an interval so that the publisher is pushing a keyframe every rtcpPLIInterval
			go func() {
				ticker := time.NewTicker(rtcpPLIInterval)
				for range ticker.C {
					checkError(pubReceiver.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: videoTrack.SSRC()}}))
				}
			}()

			rtpBuf := make([]byte, 1400)
			for {
				i, err := remoteTrack.Read(rtpBuf)
				checkError(err)
				videoTrackLock.RLock()
				_, err = videoTrack.Write(rtpBuf[:i])
				videoTrackLock.RUnlock()

				if err != io.ErrClosedPipe {
					checkError(err)
				}
			}

		} else {
			audioTrackLock.Lock()
			//var err error

			// Create a local audio track, all our SFU clients will be fed via this track

			appsrc, _ := gst.ElementFactoryMake("appsrc", "appsrc"+fmt.Sprintf("%d", atomic.LoadInt32(&pubCount)))
			appsrc.SetObject("name", "src"+fmt.Sprintf("%d", atomic.LoadInt32(&pubCount)))
			appsrc.SetObject("format", gst.NewFormat("time"))
			appsrc.SetObject("is-live", true)
			appsrc.SetObject("do-timestamp", true)

			capsfilter, _ := gst.ElementFactoryMake("capsfilter", "capsfilter"+fmt.Sprintf("%d", atomic.LoadInt32(&pubCount)))
			capsfilter.SetObject("caps", gst.CapsFromString("application/x-rtp, payload=96, encoding-name=OPUS"))

			queue, _ := gst.ElementFactoryMake("queue", "queue"+fmt.Sprintf("%d", atomic.LoadInt32(&pubCount)))
			rtpopusdepay, _ := gst.ElementFactoryMake("rtpopusdepay", "rtpopusdepay"+fmt.Sprintf("%d", atomic.LoadInt32(&pubCount)))
			decodebin, _ := gst.ElementFactoryMake("decodebin", "decodebin"+fmt.Sprintf("%d", atomic.LoadInt32(&pubCount)))
			audioconvert, _ := gst.ElementFactoryMake("audioconvert", "audioconvert"+fmt.Sprintf("%d", atomic.LoadInt32(&pubCount)))

			// audioTestSrc, _ := gst.ElementFactoryMake("audiotestsrc","testsrcaudio")
			// testConvert, _ := gst.ElementFactoryMake("audioconvert", "testconvert")

			audioTrack.SetState(gst.StateNull)
			audioTrack.AddMany(appsrc, capsfilter, queue, rtpopusdepay, decodebin, audioconvert)

			log.Println("COUNTING FROM HERE")
			log.Println(appsrc.Link(capsfilter))
			log.Println(capsfilter.Link(queue))
			log.Println(queue.Link(rtpopusdepay))
			log.Println(rtpopusdepay.Link(decodebin))

			log.Println(audioTrack.Name())
			audioMixer := audioTrack.GetByName("mix")
			log.Println(audioMixer)
			log.Println(audioconvert.Link(audioMixer)) //Main src to audiomixer

			log.Println("Counting till here")

			decodebin.SetPadAddedCallback(func(element *gst.Element, pad *gst.Pad) {
				log.Println(pad.Link(audioconvert.GetStaticPad("sink")))
			})

			audioTrack.SetState(gst.StatePlaying)
			//src := audioTrack.GetByName("src"+fmt.Sprintf("%d",atomic.LoadInt32(&pubCount)))
			if err != nil {
				log.Println("pipeline create error", err)

			}

			rtpBuf := make([]byte, 1400)
			audioTrackLock.Unlock()
			for {
				i, err := remoteTrack.Read(rtpBuf)
				audioTrackLock.Lock()
				err = appsrc.PushBuffer(rtpBuf[:i])
				audioTrackLock.Unlock()

				if err != io.ErrClosedPipe {
					checkError(err)
				} else {
					audioTrack.SetState(gst.StateNull)

				}
			}
		}
	})

	opustrack, err := pubReceiver.NewTrack(webrtc.DefaultPayloadTypeOpus, rand.Uint32(), "audio", "pion-server1")
	if err != nil {
		checkError(err)
	}
	// Add local audio track
	_, err = pubReceiver.AddTrack(opustrack)
	checkError(err)

	// Set the remote SessionDescription
	checkError(pubReceiver.SetRemoteDescription(
		webrtc.SessionDescription{
			SDP:  string(msg),
			Type: webrtc.SDPTypeOffer,
		}))

	// Create answer
	answer, err := pubReceiver.CreateAnswer(nil)
	checkError(err)

	// Sets the LocalDescription, and starts our UDP listeners
	checkError(pubReceiver.SetLocalDescription(answer))

	// Send server sdp to publisher
	checkError(c.WriteMessage(mt, []byte(answer.SDP)))

	// Register incoming channel
	pubReceiver.OnDataChannel(func(d *webrtc.DataChannel) {
		d.OnMessage(func(msg webrtc.DataChannelMessage) {
			// Broadcast the data to subSenders
			broadcastHub.broadcastChannel <- msg.Data
		})
	})

	// Sets the LocalDescription, and starts our UDP listeners

	// Send server sdp to subscriber

	// caps := gst.CapsFromString("mix.")
	// source.SetObject("caps", caps)
	// audioTrack.Add(source)

	//npl, _ := gst.ParseLaunch("audiotestsrc ! audioconvert ! appsink name=sink")

	nlp, _ := gst.PipelineNew("user")

	testsrc, _ := gst.ElementFactoryMake("filesrc", "testsrcaudio")
	testsrc.SetObject("location", "src.mp3")
	testdecodebin, _ := gst.ElementFactoryMake("decodebin", "testdecodebin")
	testaudioconvert, _ := gst.ElementFactoryMake("audioconvert", "testaudioconvert")

	testaudioresample, _ := gst.ElementFactoryMake("audioresample", "testaudioresample")
	testopus, _ := gst.ElementFactoryMake("opusenc", "opusenc")

	testsink, _ := gst.ElementFactoryMake("appsink", "sinktest")

	nlp.AddMany(testsrc, testsink, testopus, testdecodebin, testaudioconvert, testaudioresample)

	log.Println(testsrc.Link(testdecodebin))
	//log.Println(testdecodebin.Link(testaudioconvert))

	log.Println(testaudioconvert.Link(testaudioresample))
	log.Println(testaudioresample.Link(testopus))
	log.Println(testopus.Link(testsink))

	testdecodebin.SetPadAddedCallback(func(element *gst.Element, pad *gst.Pad) {
		log.Println(pad.Link(testaudioconvert.GetStaticPad("sink")))
	})

	nlp.SetState(gst.StatePlaying)
	//audioSink := npl.GetByName("sink")
	for {
		sample, err := testsink.PullSample()

		//checkError(err)
		if err != nil {
			fmt.Println("NOT NUll")
			log.Printf("err: %s", err)
			continue
			//return
		}

		//samples := uint32(48000 * (float32(sample.Duration) / 1000000000))
		if err := opustrack.WriteSample(media.Sample{
			Data:    sample.Data,
			Samples: 48000,
		}); err != nil {
			checkError(err)
		}
	}

}
