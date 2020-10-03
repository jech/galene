package rtpconn

import (
	"io"
	"log"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3"

	"sfu/packetcache"
	"sfu/rtptime"
)

func readLoop(conn *rtpUpConnection, track *rtpUpTrack) {
	writers := rtpWriterPool{conn: conn, track: track}
	defer func() {
		writers.close()
		close(track.readerDone)
	}()

	isvideo := track.track.Kind() == webrtc.RTPCodecTypeVideo
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

		first, index :=
			track.cache.Store(packet.SequenceNumber, buf[:bytes])
		if packet.SequenceNumber-first > 24 {
			found, first, bitmap := track.cache.BitmapGet()
			if found {
				err := conn.sendNACK(track, first, bitmap)
				if err != nil {
					log.Printf("%v", err)
				}
			}
		}

		_, rate := track.rate.Estimate()
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
