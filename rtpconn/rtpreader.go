package rtpconn

import (
	"io"
	"log"

	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v3"

	"sfu/packetcache"
	"sfu/rtptime"
)

func isVP8Keyframe(packet *rtp.Packet) bool {
	var vp8 codecs.VP8Packet
	_, err := vp8.Unmarshal(packet.Payload)
	if err != nil {
		return false
	}

	return vp8.S != 0 && vp8.PID == 0 &&
		len(vp8.Payload) > 0 && (vp8.Payload[0]&0x1) == 0
}

func readLoop(conn *rtpUpConnection, track *rtpUpTrack) {
	writers := rtpWriterPool{conn: conn, track: track}
	defer func() {
		writers.close()
		close(track.readerDone)
	}()

	isvideo := track.track.Kind() == webrtc.RTPCodecTypeVideo
	codec := track.track.Codec().Name
	sendNACK := track.hasRtcpFb("nack", "")
	buf := make([]byte, packetcache.BufSize)
	var packet rtp.Packet
	for {
		bytes, err := track.track.Read(buf)
		if err != nil {
			if err != io.EOF {
				log.Printf("%v", err)
			}
			break
		}
		track.rate.Accumulate(uint32(bytes))

		err = packet.Unmarshal(buf[:bytes])
		if err != nil {
			log.Printf("%v", err)
			continue
		}

		track.jitter.Accumulate(packet.Timestamp)

		kf := false
		if isvideo && codec == webrtc.VP8 {
			kf = isVP8Keyframe(&packet)
		}

		first, index := track.cache.Store(
			packet.SequenceNumber, packet.Timestamp,
			kf, packet.Marker, buf[:bytes],
		)

		_, rate := track.rate.Estimate()

		delta := packet.SequenceNumber - first
		if (delta & 0x8000) != 0 {
			delta = 0
		}
		// send a NACK if a packet is late by 65ms or 2 packets,
		// whichever is more
		packets := rate / 16
		if packets > 24 {
			packets = 24
		}
		if packets < 2 {
			packets = 2
		}
		// send NACKs for more recent packets, this makes better
		// use of the NACK bitmap
		unnacked := uint16(4)
		if unnacked > uint16(packets) {
			unnacked = uint16(packets)
		}
		if uint32(delta) > packets {
			found, first, bitmap := track.cache.BitmapGet(
				packet.SequenceNumber - unnacked,
			)
			if found && sendNACK {
				err := conn.sendNACK(track, first, bitmap)
				if err != nil {
					log.Printf("%v", err)
				}
			}
		}

		delay := uint32(rtptime.JiffiesPerSec / 1024)
		if rate > 512 {
			delay = rtptime.JiffiesPerSec / rate / 2
		}

		writers.write(packet.SequenceNumber, index, delay,
			isvideo, packet.Marker)

		select {
		case action := <-track.localCh:
			err := writers.add(action.track, action.add)
			if err != nil {
				log.Printf("add/remove track: %v", err)
			}
		default:
		}
	}
}
