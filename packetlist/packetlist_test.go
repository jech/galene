package packetlist

import (
	"bytes"
	"math/rand"
	"testing"
)

func randomBuf() []byte {
	length := rand.Int31n(BufSize-1) + 1
	buf := make([]byte, length)
	rand.Read(buf)
	return buf
}

func TestList(t *testing.T) {
	buf1 := randomBuf()
	buf2 := randomBuf()
	list := New(16)
	list.Store(13, buf1)
	list.Store(17, buf2)

	if bytes.Compare(list.Get(13), buf1) != 0 {
		t.Errorf("Couldn't get 13")
	}
	if bytes.Compare(list.Get(17), buf2) != 0 {
		t.Errorf("Couldn't get 17")
	}
	if list.Get(42) != nil {
		t.Errorf("Creation ex nihilo")
	}
}

func TestOverflow(t *testing.T) {
	list := New(16)

	for i := 0; i < 32; i++ {
		list.Store(uint16(i), []byte{uint8(i)})
	}

	for i := 0; i < 32; i++ {
		buf := list.Get(uint16(i))
		if i < 16 {
			if buf != nil {
				t.Errorf("Creation ex nihilo: %v", i)
			}
		} else {
			if len(buf) != 1 || buf[0] != uint8(i) {
				t.Errorf("Expected [%v], got %v", i, buf)
			}
		}
	}
}
