package webserver

import (
	"strings"
	"testing"
)

func TestParseSDPFrag(t *testing.T) {
	sdp := `a=ice-ufrag:FZ0m
a=ice-pwd:NRT+gj1EhsEwMm9MA7ljzBRy
m=audio 9 UDP/TLS/RTP/SAVPF 0
a=mid:0
a=candidate:2930517337 1 udp 2113937151 1eaafdf1-4127-499f-90d4-8c35ea49d5e6.local 44360 typ host generation 0 ufrag FZ0m network-cost 999
2024/09/30 00:07:41 {candidate:2930517337 1 udp 2113937151 1eaafdf1-4127-499f-90d4-8c35ea49d5e6.local 44360 typ host generation 0 ufrag FZ0m network-cost 999 0xc00062a580 0xc000620288 0xc00062a590}
a=end-of-candidates`
	r := strings.NewReader(sdp)
	ufrag, pwd, candidates, err := parseSDPFrag(r)
	if err != nil {
		t.Errorf("parseSDPFrag: %v", err)
	}
	if ufrag != "FZ0m" || pwd != "NRT+gj1EhsEwMm9MA7ljzBRy" {
		t.Errorf("Ufrag %v, pwd %v", ufrag, pwd)
	}
	if len(candidates) != 1 {
		t.Errorf("Expected 1, got %v", candidates)
	}
	if *candidates[0].SDPMLineIndex != 0 ||
		*candidates[0].SDPMid != "0" ||
		*candidates[0].UsernameFragment != "FZ0m" {
		t.Errorf("Got %v", candidates[0])
	}
}
