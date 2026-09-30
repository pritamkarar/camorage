package motion

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestParseVersion(t *testing.T) {
	for out, want := range map[string][2]int{
		"ffmpeg version 4.2.1 Copyright (c) 2000-2019 the FFmpeg developers":                {4, 2}, // the phone (Termux)
		"ffmpeg version 6.1.1-3ubuntu5+esm13 Copyright (c) 2000-2023 the FFmpeg developers": {6, 1}, // the laptop
		"ffmpeg version n7.0.2 Copyright (c) 2000-2024":                                     {7, 0},
		"ffmpeg version N-113000-gabcdef Copyright":                                         {99, 0},
		"": {99, 0},
	} {
		if maj, min := ParseVersion(out); maj != want[0] || min != want[1] {
			t.Errorf("%q: got %d.%d, want %d.%d", out, maj, min, want[0], want[1])
		}
	}
}

func TestReaderArgs(t *testing.T) {
	url := "rtsp://127.0.0.1:8554/gate_sub"
	for v, want := range map[[2]int]string{
		{4, 2}: "-nostdin -loglevel error -rtsp_transport tcp -stimeout 5000000 -skip_frame nokey -i " + url + " -vsync 0 -vf scale=64:36,format=gray -flush_packets 1 -f rawvideo pipe:1",
		{5, 0}: "-nostdin -loglevel error -rtsp_transport tcp -timeout 5000000 -skip_frame nokey -i " + url + " -vsync 0 -vf scale=64:36,format=gray -flush_packets 1 -f rawvideo pipe:1",
		{6, 1}: "-nostdin -loglevel error -rtsp_transport tcp -timeout 5000000 -skip_frame nokey -i " + url + " -fps_mode passthrough -vf scale=64:36,format=gray -flush_packets 1 -f rawvideo pipe:1",
	} {
		if got := strings.Join(ReaderArgs(v[0], v[1], url), " "); got != want {
			t.Errorf("ffmpeg %d.%d:\n got %s\nwant %s", v[0], v[1], got, want)
		}
	}
}

func TestReadFrames(t *testing.T) {
	in := append(bytes.Repeat([]byte{1}, FrameSize), bytes.Repeat([]byte{2}, FrameSize)...)
	in = append(in, 9, 9) // the start of a frame cut off when ffmpeg stopped
	var got []byte
	err := ReadFrames(bytes.NewReader(in), func(f []byte) {
		if len(f) != FrameSize {
			t.Fatalf("frame of %d bytes", len(f))
		}
		got = append(got, f[0])
	})
	if err != nil || string(got) != "\x01\x02" {
		t.Fatalf("frames %v, err %v", got, err)
	}
}

// The keyframe reader's options must be ones the installed ffmpeg accepts: Termux ships whatever
// its repo has (4.2 on Android 5/6, 8.x today). Pointed at a closed port, an ffmpeg that took every
// option fails to connect; one that did not complains about the option before trying.
// FFMPEG_ASSUME=4.2 builds the options for another version (it shows the test catches a mismatch).
func TestReaderArgsAgainstInstalledFFmpeg(t *testing.T) {
	out, err := exec.Command("ffmpeg", "-version").Output()
	if err != nil {
		t.Skip("no ffmpeg on PATH")
	}
	major, minor := ParseVersion(string(out))
	if v := os.Getenv("FFMPEG_ASSUME"); v != "" {
		fmt.Sscanf(v, "%d.%d", &major, &minor)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close() // now nothing listens there
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "ffmpeg", ReaderArgs(major, minor, "rtsp://"+addr+"/cam")...)
	cmd.Stderr = &stderr
	cmd.Run()
	msg := stderr.String()
	if strings.Contains(msg, "Unrecognized option") || strings.Contains(msg, "Option not found") || !strings.Contains(msg, "Connection refused") {
		t.Fatalf("ffmpeg (options for %d.%d) did not accept the reader's options:\n%s", major, minor, msg)
	}
}
