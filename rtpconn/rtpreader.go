package rtpconn

import (
	"io"
	"log"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3"

	"github.com/jech/galene/packetcache"
	"github.com/jech/galene/rtptime"
)

func readLoop(conn *rtpUpConnection, track *rtpUpTrack) {
	writers := rtpWriterPool{conn: conn, track: track}
	defer func() {
		writers.close()
		close(track.readerDone)
	}()

	isvideo := track.track.Kind() == webrtc.RTPCodecTypeVideo
	codec := track.track.Codec()
	sendNACK := track.hasRtcpFb("nack", "")
	buf := make([]byte, packetcache.BufSize)
	var packet rtp.Packet
	for {
		bytes, _, err := track.track.Read(buf)
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

		kf, _ := isKeyframe(codec.MimeType, &packet)

		if packet.Extension {
			packet.Extension = false
			packet.Extensions = nil
			bytes, err = packet.MarshalTo(buf)
			if err != nil {
				log.Printf("%v", err)
				continue
			}
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
		// send a NACK if a packet is late by 20ms or 2 packets,
		// whichever is more.  Since TCP sends a dupack after 2 packets,
		// this should be safe.
		packets := rate / 50
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
