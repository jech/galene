package rtpconn

import (
	"errors"
	"strings"

	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
)

// isKeyframe determines if packet is the start of a keyframe.
// It returns (true, true) if that is the case, (false, true) if that is
// definitely not the case, and (false, false) if the information cannot
// be determined.
func isKeyframe(codec string, packet *rtp.Packet) (bool, bool) {
	if strings.EqualFold(codec, "video/vp8") {
		var vp8 codecs.VP8Packet
		_, err := vp8.Unmarshal(packet.Payload)
		if err != nil || len(vp8.Payload) < 1 {
			return false, false
		}

		if vp8.S != 0 && vp8.PID == 0 && (vp8.Payload[0]&0x1) == 0 {
			return true, true
		}
		return false, true
	} else if strings.EqualFold(codec, "video/vp9") {
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
	} else if strings.EqualFold(codec, "video/av1x") {
		if len(packet.Payload) < 2 {
			return false, true
		}
		if (packet.Payload[0] & 0x88) != 0x08 {
			return false, true
		}
		w := (packet.Payload[0] & 0x30) >> 4

		getObu := func(data []byte) ([]byte, int) {
			offset := 0
			length := 0
			for {
				if len(data) <= offset {
					return nil, 0
				}
				l := data[offset]
				length = length*128 + int(l&0x7f)
				offset++
				if (l & 0x80) == 0 {
					break
				}
			}
			if len(data) < offset+length {
				return nil, 0
			}
			return data[offset : offset+length], offset + length
		}
		var obu1, obu2 []byte
		if w == 1 {
			obu1 = packet.Payload[1:]
		} else {
			var o int
			obu1, o = getObu(packet.Payload[1:])
			if len(obu1) == 0 {
				return false, false
			}
			if w == 2 {
				obu2 = packet.Payload[1+o:]
			} else {
				obu2, _ = getObu(packet.Payload[1+o:])
			}
		}
		if len(obu1) < 1 {
			return false, false
		}
		header := obu1[0]
		tpe := (header & 0x38) >> 3
		if tpe != 1 {
			return false, true
		}
		if w == 1 {
			return false, false
		}
		if len(obu2) < 1 {
			return false, false
		}
		header2 := obu2[0]
		tpe2 := (header2 & 0x38) >> 3
		if tpe2 != 6 {
			return false, false
		}
		if len(obu2) < 2 {
			return false, false
		}
		if (obu2[1] & 0x80) != 0 {
			return false, true
		}
		return (obu2[1] & 0x60) == 0, false
	} else if strings.EqualFold(codec, "video/h264") {
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
	} else {
		return false, false
	}
}

var errTruncated = errors.New("truncated packet")
var errUnsupportedCodec = errors.New("unsupported codec")

func packetFlags(codec string, buf []byte) (seqno uint16, start bool, pid uint16, tid uint8, sid uint8, layersync bool, discardable bool, err error) {
	if len(buf) < 12 {
		err = errTruncated
		return
	}

	seqno = (uint16(buf[2]) << 8) | uint16(buf[3])

	if strings.EqualFold(codec, "video/vp8") {
		var packet rtp.Packet
		err = packet.Unmarshal(buf)
		if err != nil {
			return
		}
		var vp8 codecs.VP8Packet
		_, err = vp8.Unmarshal(packet.Payload)
		if err != nil {
			return
		}

		start = vp8.S == 1 && vp8.PID == 0
		pid = vp8.PictureID
		tid = vp8.TID
		layersync = vp8.Y == 1
		discardable = vp8.N == 1
		return
	} else if strings.EqualFold(codec, "video/vp9") {
		var packet rtp.Packet
		err = packet.Unmarshal(buf)
		if err != nil {
			return
		}
		var vp9 codecs.VP9Packet
		_, err = vp9.Unmarshal(packet.Payload)
		if err != nil {
			return
		}
		start = vp9.B
		tid = vp9.TID
		sid = vp9.SID
		layersync = vp9.U
		return
	}
	return
}

func rewritePacket(codec string, data []byte, seqno uint16, delta uint16) error {
	if len(data) < 12 {
		return errTruncated
	}

	data[2] = uint8(seqno >> 8)
	data[3] = uint8(seqno)
	if delta == 0 {
		return nil
	}

	offset := 12
	offset += int(data[0]&0x0F) * 4
	if len(data) < offset+4 {
		return errTruncated
	}

	if (data[0] & 0x10) != 0 {
		length := uint16(data[offset+2])<<8 | uint16(data[offset+3])
		offset += 4 + int(length)*4
		if len(data) < offset+4 {
			return errTruncated
		}
	}

	if strings.EqualFold(codec, "video/vp8") {
		x := (data[offset] & 0x80) != 0
		if !x {
			return nil
		}
		i := (data[offset+1] & 0x80) != 0
		if !i {
			return nil
		}
		m := (data[offset+2] & 0x80) != 0
		if m {
			pid := (uint16(data[offset+2]&0x7F) << 8) |
				uint16(data[offset+3])
			pid = (pid + delta) & 0x7FFF
			data[offset+2] = 0x80 | byte((pid>>8)&0x7F)
			data[offset+3] = byte(pid & 0xFF)
		} else {
			data[offset+2] = (data[offset+2] + uint8(delta)) & 0x7F
		}
		return nil
	}
	return errUnsupportedCodec
}
