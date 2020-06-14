module github.com/hiteshjoshi/webrtc

go 1.14

require (
	github.com/gorilla/websocket v1.4.2
	github.com/hiteshjoshi/webrtc/gst v1.14.0
	github.com/notedit/gst v0.0.5 // indirect
	github.com/pion/rtcp v1.2.3
	github.com/pion/webrtc/v2 v2.2.14
	github.com/povilasv/prommod v0.0.12
	github.com/prometheus/client_golang v1.6.0
)

replace github.com/hiteshjoshi/webrtc/gst => ./gst
