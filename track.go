package main

import (
	"fmt"
	"github.com/hiteshjoshi/webrtc/gst"
	"github.com/pion/webrtc/v2"
)

var pipeline *gst.Pipeline

func createBasePipeline() {
	userPipelines = []*User{}
	p, err := gst.PipelineNew("chat")
	if err != nil {
		panic(err)
	}
	pipeline = p
}

type User struct {
	id       string
	conn     *webrtc.PeerConnection
	outTrack *webrtc.RTPSender
	tee      *gst.Element
}

func binErr(e error) {
	fmt.Println(e)
}
func binLog(e interface{}) {
	fmt.Println(e)
}
func (c *User) audioTrack(track *webrtc.Track, receiver *webrtc.RTPReceiver) {

	binDesc := fmt.Sprintf("appsrc name=%s format=time is-live=true do-timestamp=true ! application/x-rtp, payload=96, encoding-name=OPUS ! rtpopusdepay ! decodebin ! queue ! opusenc ! audioconvert", "user"+c.id)

	bin, err := gst.ParseBinFromDescription(binDesc, true)
	binErr(err)

	tee, err := gst.ElementFactoryMake("tee", "tee"+c.id)
	binErr(err)
	tee.SetObject("name", "t")
	c.tee = tee

	mixBinDesk := fmt.Sprintf("audiomixer name=%s ! audioconvert ! opusenc ! appsink name=%s", "mix"+c.id, "sink"+c.id)
	mixBin, err := gst.ParseBinFromDescription(mixBinDesk, true)
	binErr(err)

	pipeline.AddMany(&bin.Element, tee, &mixBin.Element)

	binLog(bin.Link(tee))

	userPipelines = append(userPipelines, c)

	for _, user := range userPipelines {
		if user == c {
			continue
		}

	}

	//This bin will take user input and convert it to opus media
	//App src this will read the stream from opustrack

	//Now open enc media should be sent to other users pipelines

	src := bin.GetByName("user" + c.id)
	//continuously read the stream
	rtpBuf := make([]byte, 1400)
	for {
		i, err := track.Read(rtpBuf)
		binErr(err)
		err = src.PushBuffer(rtpBuf[:i])
		binErr(err)

	}
}

func (c *User) videoTrack(track *webrtc.Track, receiver *webrtc.RTPReceiver) {
	return
}

func (c *User) gotTrack(remoteTrack *webrtc.Track, receiver *webrtc.RTPReceiver) {
	if remoteTrack.PayloadType() == webrtc.DefaultPayloadTypeVP8 || remoteTrack.PayloadType() == webrtc.DefaultPayloadTypeVP9 || remoteTrack.PayloadType() == webrtc.DefaultPayloadTypeH264 {

		go c.videoTrack(remoteTrack, receiver)
		return

	} else {
		go c.audioTrack(remoteTrack, receiver)
		return

	}

}
