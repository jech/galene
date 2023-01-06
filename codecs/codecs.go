package codecs

import (
	"errors"
	"strings"

	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
)

var errTruncated = errors.New("truncated packet")

// Keyframe determines if packet is the start of a keyframe.
// It returns (true, true) if that is the case, (false, true) if that is
// definitely not the case, and (false, false) if the information cannot
// be determined.
func Keyframe(codec string, packet *rtp.Packet) (bool, bool) {
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
	} else if strings.EqualFold(codec, "video/av1") {
		if len(packet.Payload) < 2 {
			return false, true
		}
		// Z=0, N=1
		if (packet.Payload[0] & 0x88) != 0x08 {
			return false, true
		}
		w := (packet.Payload[0] & 0x30) >> 4

		getObu := func(data []byte, last bool) ([]byte, int, bool) {
			if last {
				return data, len(data), false
			}
			offset := 0
			length := 0
			for {
				if len(data) <= offset {
					return nil, offset, offset > 0
				}
				l := data[offset]
				length |= int(l&0x7f) << (offset * 7)
				offset++
				if (l & 0x80) == 0 {
					break
				}
			}
			if len(data) < offset+length {
				return data[offset:], len(data), true
			}
			return data[offset : offset+length],
				offset + length, false
		}
		offset := 1
		i := 0
		for {
			obu, length, truncated :=
				getObu(packet.Payload[offset:], int(w) == i+1)
			if len(obu) < 1 {
				return false, false
			}
			tpe := (obu[0] & 0x38) >> 3
			switch i {
			case 0:
				// OBU_SEQUENCE_HEADER
				if tpe != 1 {
					return false, true
				}
			default:
				// OBU_FRAME_HEADER or OBU_FRAME
				if tpe == 3 || tpe == 6 {
					if len(obu) < 2 {
						return false, false
					}
					// show_existing_frame == 0
					if (obu[1] & 0x80) != 0 {
						return false, true
					}
					// frame_type == KEY_FRAME
					return (obu[1] & 0x60) == 0, true
				}
			}
			if truncated || i >= int(w) {
				// the first frame header is in a second
				// packet, give up.
				return false, false
			}
			offset += length
			i++
		}
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
			return nalu == 7, true
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
				if n == 7 {
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
			return (packet.Payload[1]&0x1F == 7), true
		}
		return false, false
	}
	return false, false
}

func KeyframeDimensions(codec string, packet *rtp.Packet) (uint32, uint32) {
	if strings.EqualFold(codec, "video/vp8") {
		var vp8 codecs.VP8Packet
		_, err := vp8.Unmarshal(packet.Payload)
		if err != nil {
			return 0, 0
		}
		if len(vp8.Payload) < 10 {
			return 0, 0
		}
		raw := uint32(vp8.Payload[6]) | uint32(vp8.Payload[7])<<8 |
			uint32(vp8.Payload[8])<<16 | uint32(vp8.Payload[9])<<24
		width := raw & 0x3FFF
		height := (raw >> 16) & 0x3FFF
		return width, height
	} else if strings.EqualFold(codec, "video/vp9") {
		if packet == nil {
			return 0, 0
		}
		var vp9 codecs.VP9Packet
		_, err := vp9.Unmarshal(packet.Payload)
		if err != nil {
			return 0, 0
		}
		if !vp9.V {
			return 0, 0
		}
		w := uint32(0)
		h := uint32(0)
		for i := range vp9.Width {
			if i >= len(vp9.Height) {
				break
			}
			if w < uint32(vp9.Width[i]) {
				w = uint32(vp9.Width[i])
			}
			if h < uint32(vp9.Height[i]) {
				h = uint32(vp9.Height[i])
			}
		}
		return w, h
	} else {
		return 0, 0
	}
}

type Flags struct {
	Seqno           uint16
	Marker          bool
	Start           bool
	End             bool
	Keyframe        bool
	Pid             uint16 // only returned for VP8
	Tid             uint8
	Sid             uint8
	TidUpSync       bool
	SidUpSync       bool
	SidNonReference bool
	Discardable     bool
}

func PacketFlags(codec string, buf []byte) (Flags, error) {
	if len(buf) < 4 {
		return Flags{}, errTruncated
	}

	var flags Flags

	flags.Seqno = (uint16(buf[2]) << 8) | uint16(buf[3])
	flags.Marker = (buf[1] & 0x80) != 0

	if strings.EqualFold(codec, "video/vp8") {
		var packet rtp.Packet
		err := packet.Unmarshal(buf)
		if err != nil {
			return flags, err
		}
		var vp8 codecs.VP8Packet
		_, err = vp8.Unmarshal(packet.Payload)
		if err != nil {
			return flags, err
		}

		flags.Start = vp8.S != 0 && vp8.PID == 0
		flags.End = packet.Marker
		flags.Keyframe = vp8.S != 0 && (vp8.Payload[0]&0x1) == 0
		flags.Pid = vp8.PictureID
		flags.Tid = vp8.TID
		flags.TidUpSync = flags.Keyframe || vp8.Y == 1
		flags.SidUpSync = flags.Keyframe
		flags.Discardable = vp8.N == 1
		return flags, nil
	} else if strings.EqualFold(codec, "video/vp9") {
		var packet rtp.Packet
		err := packet.Unmarshal(buf)
		if err != nil {
			return flags, err
		}
		var vp9 codecs.VP9Packet
		_, err = vp9.Unmarshal(packet.Payload)
		if err != nil {
			return flags, err
		}
		flags.Start = vp9.B
		flags.End = vp9.E
		if (vp9.Payload[0] & 0xc0) == 0x80 {
			profile := (vp9.Payload[0] >> 4) & 0x3
			if profile != 3 {
				flags.Keyframe = (vp9.Payload[0] & 0xC) == 0
			} else {
				flags.Keyframe = (vp9.Payload[0] & 0x6) == 0
			}
		}
		flags.Tid = vp9.TID
		flags.Sid = vp9.SID
		flags.TidUpSync = flags.Keyframe || vp9.U
		flags.SidUpSync = flags.Keyframe || !vp9.P
		flags.SidNonReference = (packet.Payload[0] & 0x01) != 0
		return flags, nil
	}
	return flags, nil
}

func RewritePacket(codec string, data []byte, setMarker bool, seqno uint16, delta uint16) error {
	if len(data) < 12 {
		return errTruncated
	}

	if setMarker {
		data[1] |= 0x80
	}

	data[2] = uint8(seqno >> 8)
	data[3] = uint8(seqno)
	if delta == 0 {
		return nil
	}

	offset := 12
	offset += int(data[0]&0x0F) * 4
	if len(data) <= offset {
		return errTruncated
	}

	if (data[0] & 0x10) != 0 {
		length := uint16(data[offset+2])<<8 | uint16(data[offset+3])
		offset += 4 + int(length)*4
		if len(data) < offset+4 {
			return errTruncated
		}
	}

	// only rewrite PID for VP8.
	if strings.EqualFold(codec, "video/vp8") {
		x := (data[offset] & 0x80) != 0
		if !x {
			return nil
		}
		offset++
		if len(data) <= offset {
			return errTruncated
		}
		i := (data[offset] & 0x80) != 0
		if !i {
			return nil
		}
		offset++
		if len(data) <= offset {
			return errTruncated
		}
		m := (data[offset] & 0x80) != 0
		if m {
			if len(data) <= offset+1 {
				return errTruncated
			}
			pid := (uint16(data[offset]&0x7F) << 8) |
				uint16(data[offset+1])
			pid = (pid + delta) & 0x7FFF
			data[offset] = 0x80 | byte((pid>>8)&0x7F)
			data[offset+1] = byte(pid & 0xFF)
		} else {
			data[offset] = (data[offset] + uint8(delta)) & 0x7F
		}
		return nil
	}

	return nil
}
