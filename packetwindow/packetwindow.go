package packetwindow

import (
	"fmt"
)

type Window struct {
	first  uint16
	bitmap uint32
}

func New() *Window {
	return &Window{}
}

func (w *Window) String() string {
	buf := make([]byte, 32)
	for i := 0; i < 32; i++ {
		if (w.bitmap & (1 << i)) != 0 {
			buf[i] = '1'
		} else {
			buf[i] = '0'
		}
	}

	return fmt.Sprintf("[%04x %s]", w.first, buf)
}

func (w *Window) First() uint16 {
	return w.first
}

func (w *Window) Reset() {
	w.bitmap = 0
}

func (w *Window) Set(seqno uint16) {
	if w.bitmap == 0 {
		w.first = seqno
		w.bitmap = 1
		return
	}

	if ((seqno - w.first) & 0x8000) != 0 {
		return
	}

	if seqno == w.first {
		w.bitmap >>= 1
		w.first += 1
		for (w.bitmap & 1) == 1 {
			w.bitmap >>= 1
			w.first += 1
		}
		return
	}

	if seqno - w.first < 32 {
		w.bitmap |= (1 << uint16(seqno - w.first))
		return
	}

	shift := seqno - w.first - 31
	w.bitmap >>= shift
	w.first += shift
	w.bitmap |= (1 << uint16(seqno - w.first))
	return
}

func (w *Window) Get17() (uint16, uint16) {
	first := w.first
	bitmap := uint16((w.bitmap >> 1) & 0xFFFF)
	w.bitmap >>= 17
	w.first += 17
	return first, bitmap
}
