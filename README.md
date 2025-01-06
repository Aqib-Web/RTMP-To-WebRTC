# RTP to WebRTC Streaming Application

This project re-streams media from a source to WebRTC using a Go server and FFmpeg for RTP streaming. It provides real-time audio and video from a specified media file to a WebRTC client in the browser, using WebSockets for automated SDP and ICE exchange.

## Features
- Streams audio and video from a media file (e.g., .mp4) to a WebRTC client.
- WebSocket-based signaling for automated SDP and ICE candidate exchange.
- Configurable RTP ports for audio and video ingestion.
- Improved error handling and logging for production-ready robustness.
- Includes a TURN server (Pion Simple TURN Server) for NAT traversal and enhanced connectivity.


## Prerequisites

Before getting started, make sure you have the following tools installed:

- Go (Golang), , download it from [the Go website](https://golang.org/dl/).
- ffmpeg, [Get ffmpeg](https://www.ffmpeg.org/download.html).

## Installation

To install the required dependencies, follow these steps:

1. **Clone the repository**
2. **Add these modules**
   ```bash
   go get github.com/pion/webrtc/v4
   go get github.com/pion/rtp
   go get github.com/gorilla/websocket

### 1. Start server

```bash
go run main.go
```

### 2. Run Turn server

```bash
cd turn
go build turn-server.go
./turn-server -public-ip 127.0.0.1 -users username=password
```

### 3. ffmpeg

###### For Media file (.mp4)
```bash
ffmpeg -re -i input.mp4 -map 0:v -c:v libvpx -deadline realtime -quality realtime -cpu-used 5 -bufsize 1000k -g 15 -r 30 -b:v 800k -static-thresh 0 -error-resilient 1 -max_delay 0 -buffer_size 0 -payload_type 96 -ssrc 1 -f rtp rtp://127.0.0.1:5004 -map 0:a -c:a libopus -b:a 48k -application lowdelay -frame_duration 20 -payload_type 111 -ssrc 2 -f rtp rtp://127.0.0.1:5005
```

###### For Webcam and Microphone
```bash
ffmpeg -f dshow -i video="Camera name":audio="Mic name" -map 0:v -c:v libvpx -deadline realtime -quality realtime -cpu-used 5 -bufsize 1000k -g 15 -r 30 -b:v 800k -s 640x480 -static-thresh 0 -error-resilient 1 -payload_type 96 -ssrc 1 -f rtp rtp://127.0.0.1:5004 -map 0:a -c:a libopus -b:a 48k -payload_type 111 -ssrc 2 -f rtp rtp://127.0.0.1:5005
```

###### For RTMP Link
```bash
ffmpeg -re -i rtmp://your-rtmp-server/stream-key -map 0:v -c:v libvpx -deadline realtime -quality realtime -cpu-used 5 -bufsize 1000k -g 15 -r 30 -b:v 2M -s 1280x720 -static-thresh 0 -error-resilient 1 -payload_type 96 -ssrc 1 -f rtp rtp://127.0.0.1:5004 -map 0:a -c:a libopus -b:a 48k -application lowdelay -frame_duration 20 -payload_type 111 -ssrc 2 -f rtp rtp://127.0.0.1:5005
```

#### Note:

Replace input.mp4/rtmp_url with the path to your media file. (a file named input.mp4 is already included for testing)



### Packages used

This project uses the following Go packages:

- [`github.com/pion/webrtc/v4`](https://github.com/pion/webrtc) – A WebRTC API implementation for Go.
- [`github.com/pion/rtp`](https://github.com/pion/rtp) – RTP (Real-Time Protocol) handling for Go.
- [`github.com/pion/turn`](https://github.com/pion/turn) – A toolkit for building TURN servers in Go
- [`github.com/gorilla/websocket`](https://github.com/gorilla/websocket) – A WebSocket implementation for Go.
