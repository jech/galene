package sdpfrag

import (
	"testing"
	"reflect"

	"github.com/pion/sdp/v3"
)

var sdpOffer = `v=0
o=- 999478090166020379 2 IN IP4 127.0.0.1
s=-
t=0 0
a=group:BUNDLE 0 1
a=extmap-allow-mixed
a=msid-semantic: WMS ef5d4db1-5c64-4b6e-9f97-8b1f3e598bfe
m=audio 9 UDP/TLS/RTP/SAVPF 111 63 9 0 8 13 110 126
c=IN IP4 0.0.0.0
a=rtcp:9 IN IP4 0.0.0.0
a=ice-ufrag:qiKa
a=ice-pwd:bcfs93hb/+ZLuUE2K50HVbkr
a=ice-options:trickle
a=fingerprint:sha-256 EF:FE:1C:DA:83:C0:AF:B3:12:31:42:32:A4:37:04:5A:BE:7A:8D:BA:9D:0B:F2:A0:81:17:51:60:F4:96:11:5D
a=setup:actpass
a=mid:0
a=extmap:1 urn:ietf:params:rtp-hdrext:ssrc-audio-level
a=extmap:2 http://www.webrtc.org/experiments/rtp-hdrext/abs-send-time
a=extmap:3 http://www.ietf.org/id/draft-holmer-rmcat-transport-wide-cc-extensions-01
a=extmap:4 urn:ietf:params:rtp-hdrext:sdes:mid
a=sendrecv
a=msid:ef5d4db1-5c64-4b6e-9f97-8b1f3e598bfe d71aa4c4-6f2a-480a-9c83-75e95ef1519c
a=rtcp-mux
a=rtcp-rsize
a=rtpmap:111 opus/48000/2
a=rtcp-fb:111 transport-cc
a=fmtp:111 minptime=10;useinbandfec=1
a=rtpmap:63 red/48000/2
a=fmtp:63 111/111
a=rtpmap:9 G722/8000
a=rtpmap:0 PCMU/8000
a=rtpmap:8 PCMA/8000
a=rtpmap:13 CN/8000
a=rtpmap:110 telephone-event/48000
a=rtpmap:126 telephone-event/8000
a=ssrc:870576534 cname:VkgQMWvIx9VqAt+A
a=ssrc:870576534 msid:ef5d4db1-5c64-4b6e-9f97-8b1f3e598bfe d71aa4c4-6f2a-480a-9c83-75e95ef1519c
m=video 9 UDP/TLS/RTP/SAVPF 96 97 103 104 107 108 109 114 115 116 117 118 39 40 45 46 98 99 100 101 119 120 121
c=IN IP4 0.0.0.0
a=rtcp:9 IN IP4 0.0.0.0
a=ice-ufrag:qiKa
a=ice-pwd:bcfs93hb/+ZLuUE2K50HVbkr
a=ice-options:trickle
a=fingerprint:sha-256 EF:FE:1C:DA:83:C0:AF:B3:12:31:42:32:A4:37:04:5A:BE:7A:8D:BA:9D:0B:F2:A0:81:17:51:60:F4:96:11:5D
a=setup:actpass
a=mid:1
a=extmap:14 urn:ietf:params:rtp-hdrext:toffset
a=extmap:2 http://www.webrtc.org/experiments/rtp-hdrext/abs-send-time
a=extmap:13 urn:3gpp:video-orientation
a=extmap:3 http://www.ietf.org/id/draft-holmer-rmcat-transport-wide-cc-extensions-01
a=extmap:5 http://www.webrtc.org/experiments/rtp-hdrext/playout-delay
a=extmap:6 http://www.webrtc.org/experiments/rtp-hdrext/video-content-type
a=extmap:7 http://www.webrtc.org/experiments/rtp-hdrext/video-timing
a=extmap:8 http://www.webrtc.org/experiments/rtp-hdrext/color-space
a=extmap:4 urn:ietf:params:rtp-hdrext:sdes:mid
a=extmap:10 urn:ietf:params:rtp-hdrext:sdes:rtp-stream-id
a=extmap:11 urn:ietf:params:rtp-hdrext:sdes:repaired-rtp-stream-id
a=sendrecv
a=msid:ef5d4db1-5c64-4b6e-9f97-8b1f3e598bfe 4fada03b-35ae-435d-b08e-001b6ce8362f
a=rtcp-mux
a=rtcp-rsize
a=rtpmap:96 VP8/90000
a=rtcp-fb:96 goog-remb
a=rtcp-fb:96 transport-cc
a=rtcp-fb:96 ccm fir
a=rtcp-fb:96 nack
a=rtcp-fb:96 nack pli
a=rtpmap:97 rtx/90000
a=fmtp:97 apt=96
a=rtpmap:103 H264/90000
a=rtcp-fb:103 goog-remb
a=rtcp-fb:103 transport-cc
a=rtcp-fb:103 ccm fir
a=rtcp-fb:103 nack
a=rtcp-fb:103 nack pli
a=fmtp:103 level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42001f
a=rtpmap:104 rtx/90000
a=fmtp:104 apt=103
a=rtpmap:107 H264/90000
a=rtcp-fb:107 goog-remb
a=rtcp-fb:107 transport-cc
a=rtcp-fb:107 ccm fir
a=rtcp-fb:107 nack
a=rtcp-fb:107 nack pli
a=fmtp:107 level-asymmetry-allowed=1;packetization-mode=0;profile-level-id=42001f
a=rtpmap:108 rtx/90000
a=fmtp:108 apt=107
a=rtpmap:109 H264/90000
a=rtcp-fb:109 goog-remb
a=rtcp-fb:109 transport-cc
a=rtcp-fb:109 ccm fir
a=rtcp-fb:109 nack
a=rtcp-fb:109 nack pli
a=fmtp:109 level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f
a=rtpmap:114 rtx/90000
a=fmtp:114 apt=109
a=rtpmap:115 H264/90000
a=rtcp-fb:115 goog-remb
a=rtcp-fb:115 transport-cc
a=rtcp-fb:115 ccm fir
a=rtcp-fb:115 nack
a=rtcp-fb:115 nack pli
a=fmtp:115 level-asymmetry-allowed=1;packetization-mode=0;profile-level-id=42e01f
a=rtpmap:116 rtx/90000
a=fmtp:116 apt=115
a=rtpmap:117 H264/90000
a=rtcp-fb:117 goog-remb
a=rtcp-fb:117 transport-cc
a=rtcp-fb:117 ccm fir
a=rtcp-fb:117 nack
a=rtcp-fb:117 nack pli
a=fmtp:117 level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=4d001f
a=rtpmap:118 rtx/90000
a=fmtp:118 apt=117
a=rtpmap:39 H264/90000
a=rtcp-fb:39 goog-remb
a=rtcp-fb:39 transport-cc
a=rtcp-fb:39 ccm fir
a=rtcp-fb:39 nack
a=rtcp-fb:39 nack pli
a=fmtp:39 level-asymmetry-allowed=1;packetization-mode=0;profile-level-id=4d001f
a=rtpmap:40 rtx/90000
a=fmtp:40 apt=39
a=rtpmap:45 AV1/90000
a=rtcp-fb:45 goog-remb
a=rtcp-fb:45 transport-cc
a=rtcp-fb:45 ccm fir
a=rtcp-fb:45 nack
a=rtcp-fb:45 nack pli
a=fmtp:45 level-idx=5;profile=0;tier=0
a=rtpmap:46 rtx/90000
a=fmtp:46 apt=45
a=rtpmap:98 VP9/90000
a=rtcp-fb:98 goog-remb
a=rtcp-fb:98 transport-cc
a=rtcp-fb:98 ccm fir
a=rtcp-fb:98 nack
a=rtcp-fb:98 nack pli
a=fmtp:98 profile-id=0
a=rtpmap:99 rtx/90000
a=fmtp:99 apt=98
a=rtpmap:100 VP9/90000
a=rtcp-fb:100 goog-remb
a=rtcp-fb:100 transport-cc
a=rtcp-fb:100 ccm fir
a=rtcp-fb:100 nack
a=rtcp-fb:100 nack pli
a=fmtp:100 profile-id=2
a=rtpmap:101 rtx/90000
a=fmtp:101 apt=100
a=rtpmap:119 red/90000
a=rtpmap:120 rtx/90000
a=fmtp:120 apt=119
a=rtpmap:121 ulpfec/90000
a=ssrc-group:FID 18877847 3714356054
a=ssrc:18877847 cname:VkgQMWvIx9VqAt+A
a=ssrc:18877847 msid:ef5d4db1-5c64-4b6e-9f97-8b1f3e598bfe 4fada03b-35ae-435d-b08e-001b6ce8362f
a=ssrc:3714356054 cname:VkgQMWvIx9VqAt+A
a=ssrc:3714356054 msid:ef5d4db1-5c64-4b6e-9f97-8b1f3e598bfe 4fada03b-35ae-435d-b08e-001b6ce8362f
`

var sdpfragTrickle = `a=ice-ufrag:qiKa
a=ice-pwd:bcfs93hb/+ZLuUE2K50HVbkr
m=audio 9 UDP/TLS/RTP/SAVPF 0
a=mid:0
a=candidate:1937612317 1 udp 2113937151 40a192af-d71f-4cf2-b5b4-494a95dcf739.local 44501 typ host generation 0 ufrag qiKa network-cost 999
a=candidate:670660484 1 udp 2113939711 47da31ec-00cb-4c68-bc90-c403b203d8d8.local 45749 typ host generation 0 ufrag qiKa network-cost 999
`

var sdpfragEndOfCandidates = `a=ice-ufrag:qiKa
a=ice-pwd:bcfs93hb/+ZLuUE2K50HVbkr
m=audio 9 UDP/TLS/RTP/SAVPF 0
a=mid:0
a=end-of-candidates
`

var sdpfragRestart = `a=ice-ufrag:HWmk
a=ice-pwd:6kyIjoI1lVhYJUeo1EyUq0Ei
`

func TestParseSDPFrag(t *testing.T) {
	var frag SDPFrag
	err := frag.Unmarshal([]byte(sdpfragTrickle))
	if err != nil {
		t.Errorf("Unmarshal: %v", err)
	}
	if frag.UsernameFragment != "qiKa" ||
		frag.Password != "bcfs93hb/+ZLuUE2K50HVbkr" {
		t.Errorf("Ufrag %v, pwd %v", frag.UsernameFragment, frag.Password)
	}
	if len(frag.Candidates) != 0 {
		t.Errorf("Expected 0, got %v", len(frag.Candidates))
	}
	if len(frag.MediaDescriptions) != 1 {
		t.Errorf("Expected 1, got %v", len(frag.MediaDescriptions))
	}
	if len(frag.MediaDescriptions[0].Candidates) != 2 {
		t.Errorf("Expected 1, got %v", len(frag.MediaDescriptions[0].Candidates))
	}
	c := frag.MediaDescriptions[0].Candidates[0]
	if *c.SDPMLineIndex != 0 ||
		*c.SDPMid != "0" ||
		*c.UsernameFragment != "qiKa" {
		t.Errorf("Got %v", c)
	}
}

func testRoundtrip(t *testing.T, sdpfrag string) {
	var frag SDPFrag
	err := frag.Unmarshal([]byte(sdpfrag))
	if err != nil {
		t.Errorf("Unmarshal: %v", err)
	}

	sdpfrag2, err := frag.Marshal()
	if err != nil {
		t.Errorf("Marshal: %v", err)
	}

	var frag2 SDPFrag
	err = frag2.Unmarshal(sdpfrag2)
	if err != nil {
		t.Errorf("Unmarshal: %v", err)
	}

	if !reflect.DeepEqual(frag, frag2) {
		t.Errorf("Not equal: %v %v", frag, frag2)
	}
}

func TestRoundtrip(t *testing.T) {
	type test struct { name, frag string }
	for _, tt := range []test{
		{"sdpfragTrickle", sdpfragTrickle},
		{"sdpfragEndOfCandidates", sdpfragEndOfCandidates},
		{"sdpfragRestart", sdpfragRestart},
	} {
		t.Run(tt.name, func(t *testing.T) {
			testRoundtrip(t, tt.frag)
		})
	}
}

func testPatchSDP(t *testing.T, sdpOffer, sdpFrag, ufrag string, ncandidates int) {
	var s sdp.SessionDescription
	err := s.Unmarshal([]byte(sdpOffer))
	if err != nil {
		t.Fatalf("Unmarshal SDP: %v", err)
	}

	var f SDPFrag
	err = f.Unmarshal([]byte(sdpFrag))
	if err != nil {
		t.Fatalf("Unmarshal frag: %v", err)
	}

	ss, _ := PatchSDP(s, f)
	u, _ := ss.MediaDescriptions[0].Attribute("ice-ufrag")
	if u != ufrag {
		t.Errorf("UFrag mismatch: %v and %v", u, ufrag)
	}

	nc := 0
	for _, a := range ss.Attributes {
		if a.Key == "candidate" {
			nc++
		}
	}

	for _, m := range ss.MediaDescriptions {
		for _, a := range m.Attributes {
			if a.Key == "candidate" {
				nc++
			}
		}
	}
	if nc != ncandidates {
		t.Errorf("Candidate mismatch: got %v, expected %v",
			nc, ncandidates)
	}
}

func TestPatchSDP(t *testing.T) {
	t.Run("trickle", func(t *testing.T) {
		testPatchSDP(t, sdpOffer, sdpfragTrickle, "qiKa", 2)
	})
	t.Run("end-of-candidates", func(t *testing.T) {
		testPatchSDP(t, sdpOffer, sdpfragEndOfCandidates, "qiKa", 0)
	})
	t.Run("restart", func(t *testing.T) {
		testPatchSDP(t, sdpOffer, sdpfragRestart, "HWmk", 0)
	})
}
