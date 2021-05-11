package rtpconn

import (
	"strings"

	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
)

// isKeyframe determines if packet is the start of a keyframe.
// It returns (true, true) if that is the case, (false, true) if that is
// definitely not the case, and (false, false) if the information cannot
// be determined.
func isKeyframe(codec string, packet *rtp.Packet) (bool, bool) {
	switch strings.ToLower(codec) {
	case "video/vp8":
		var vp8 codecs.VP8Packet
		_, err := vp8.Unmarshal(packet.Payload)
		if err != nil || len(vp8.Payload) < 1 {
			return false, false
		}

		if vp8.S != 0 && vp8.PID == 0 && (vp8.Payload[0]&0x1) == 0 {
			return true, true
		}
		return false, true
	case "video/vp9":
		var vp9 codecs.VP9Packet
		_, err := vp9.Unmarshal(packet.Payload)
		if err != nil || len(vp9.Payload) < 1 {
			return false, false
		}
		if !vp9.B {
			return false, true
		}

		if (vp9.Payload[0] & 0xc0) != 0x80 {
			return false, false
		}

		profile := (vp9.Payload[0] >> 4) & 0x3
		if profile != 3 {
			return (vp9.Payload[0] & 0xC) == 0, true
		}
		return (vp9.Payload[0] & 0x6) == 0, true
	case "video/h264":
		if len(packet.Payload) < 1 {
			return false, false
		}
		nalu := packet.Payload[0] & 0x1F
		if nalu == 0 {
			// reserved
			return false, false
		} else if nalu <= 23 {
			// simple NALU
			return nalu == 5, true
		} else if nalu == 24 || nalu == 25 || nalu == 26 || nalu == 27 {
			// STAP-A, STAP-B, MTAP16 or MTAP24
			i := 1
			if nalu == 25 || nalu == 26 || nalu == 27 {
				// skip DON
				i += 2
			}
			for i < len(packet.Payload) {
				if i+2 > len(packet.Payload) {
					return false, false
				}
				length := uint16(packet.Payload[i])<<8 |
					uint16(packet.Payload[i+1])
				i += 2
				if i+int(length) > len(packet.Payload) {
					return false, false
				}
				offset := 0
				if nalu == 26 {
					offset = 3
				} else if nalu == 27 {
					offset = 4
				}
				if offset >= int(length) {
					return false, false
				}
				n := packet.Payload[i+offset] & 0x1F
				if n == 5 {
					return true, true
				} else if n >= 24 {
					// is this legal?
					return false, false
				}
				i += int(length)
			}
			if i == len(packet.Payload) {
				return false, true
			}
			return false, false
		} else if nalu == 28 || nalu == 29 {
			// FU-A or FU-B
			if len(packet.Payload) < 2 {
				return false, false
			}
			if (packet.Payload[1] & 0x80) == 0 {
				// not a starting fragment
				return false, true
			}
			return (packet.Payload[1]&0x1F == 5), true
		}
		return false, false

	default:
		return false, false
	}
}
