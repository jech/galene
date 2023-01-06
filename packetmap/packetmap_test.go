package packetmap

import (
	"testing"
)

func TestNoDrops(t *testing.T) {
	m := Map{}

	ok, s, p := m.Map(42, 1001)
	if !ok || s != 42 || p != 0 {
		t.Errorf("Expected 42, 0, got %v, %v, %v", ok, s, p)
	}

	ok, s, p = m.Map(43, 1001)
	if !ok || s != 43 || p != 0 {
		t.Errorf("Expected 43, 0, got %v, %v, %v", ok, s, p)
	}

	ok, s, p = m.Map(44, 1002)
	if !ok || s != 44 || p != 0 {
		t.Errorf("Expected 43, 0, got %v, %v, %v", ok, s, p)
	}

	ok, s, p = m.Map(40, 1000)
	if !ok || s != 40 || p != 0 {
		t.Errorf("Expected 40, 0, got %v, %v, %v", ok, s, p)
	}

	if len(m.entries) > 0 || m.delta != 0 || m.pidDelta != 0 {
		t.Errorf("Expected 0, got %v %v %v",
			len(m.entries), m.delta, m.pidDelta)
	}
}

func TestReorder(t *testing.T) {
	m := Map{}

	ok, s, p := m.Map(42, 1001)
	if !ok || s != 42 || p != 0 {
		t.Errorf("Expected 42, 0, got %v, %v, %v", ok, s, p)
	}

	ok = m.Drop(43, 1002)
	if !ok {
		t.Errorf("Expected ok")
	}

	ok, s, p = m.Map(45, 1003)
	if !ok || s != 44 || p != 1 {
		t.Errorf("Expected 44, 1, got %v, %v, %v", ok, s, p)
	}

	ok, s, p = m.Map(44, 1003)
	if !ok || s != 43 || p != 1 {
		t.Errorf("Expected 43, 0, got %v, %v, %v", ok, s, p)
	}
}

func TestDrop(t *testing.T) {
	m := Map{}

	ok, s, p := m.Map(42, 1001)
	if !ok || s != 42 || p != 0 {
		t.Errorf("Expected 42, 0, got %v, %v, %v", ok, s, p)
	}

	ok = m.Drop(43, 1001)
	if !ok || m.pidDelta != 0 {
		t.Errorf("Expected 0, got %v, %v", ok, m.pidDelta)
	}

	ok, s, p = m.Map(44, 1001)
	if !ok || s != 43 || p != 0 {
		t.Errorf("Expected 43, 0, got %v, %v, %v", ok, s, p)
	}

	ok, s, p = m.Map(45, 1002)
	if !ok || s != 44 || p != 0 {
		t.Errorf("Expected 44, 0, got %v, %v, %v", ok, s, p)
	}

	ok = m.Drop(46, 1003)
	if !ok || m.pidDelta != 1 {
		t.Errorf("Expected 1, got %v, %v", ok, m.pidDelta)
	}

	ok, s, p = m.Map(47, 1003)
	if !ok || s != 45 || p != 1 {
		t.Errorf("Expected 45, 1, got %v, %v, %v", ok, s, p)
	}

	ok = m.Drop(48, 1003)
	if !ok || m.pidDelta != 1 {
		t.Errorf("Expected 1, got %v, %v", ok, m.pidDelta)
	}

	ok, s, p = m.Map(49, 1003)
	if !ok || s != 46 || p != 1 {
		t.Errorf("Expected 45, 1, got %v, %v, %v", ok, s, p)
	}

	ok, s, p = m.Map(60, 1007)
	if !ok || s != 57 || p != 1 {
		t.Errorf("Expected 57, 1, got %v, %v, %v", ok, s, p)
	}

	ok, s, p = m.Map(13, 1000)
	if !ok || s != 13 || p != 0 {
		t.Errorf("Expected 13, 0, got %v, %v, %v", ok, s, p)
	}

	ok, s, p = m.Map(44, 1001)
	if !ok || s != 43 || p != 0 {
		t.Errorf("Expected 43, 0, got %v, %v, %v", ok, s, p)
	}

	ok, s, p = m.Map(45, 1002)
	if !ok || s != 44 || p != 0 {
		t.Errorf("Expected 44, 0, got %v, %v, %v", ok, s, p)
	}

	ok, s, p = m.Map(48, 3)
	if ok {
		t.Errorf("Expected not ok")
	}

	ok, s, p = m.direct(1000)
	if ok {
		t.Errorf("Expected not ok")
	}

	ok, s, p = m.direct(13)
	if !ok || s != 13 || p != 0 {
		t.Errorf("Expected 13, 0, got %v, %v, %v", ok, s, p)
	}

	ok, s, p = m.Reverse(44)
	if !ok || s != 45 || p != 0 {
		t.Errorf("Expected 45, 0, got %v %v %v", ok, s, p)
	}
}

func TestWraparound(t *testing.T) {
	m := Map{}

	ok, s, p := m.Map(0, 0)
	if !ok || s != 0 || p != 0 {
		t.Errorf("Expected 0, 0, got %v, %v, %v", ok, s, p)
	}

	ok, s, p = m.Map(1, 0)
	if !ok || s != 1 || p != 0 {
		t.Errorf("Expected 1, 0, got %v, %v, %v", ok, s, p)
	}

	ok = m.Drop(2, 1)
	if !ok || m.pidDelta != 1 {
		t.Errorf("Expected 1, got %v, %v", ok, m.pidDelta)
	}

	ok = m.Drop(3, 1)
	if !ok || m.pidDelta != 1 {
		t.Errorf("Expected 1, got %v, %v", ok, m.pidDelta)
	}

	for i := 4; i < 256000; i++ {
		ok, s, p = m.Map(uint16(i), uint16((i/2) & 0x7FFF))
		if !ok || s != uint16(i-2) || p != 1 {
			t.Errorf("Expected %v, %v, got %v, %v, %v",
				uint16(i-2), 1, ok, s, p)
		}
	}

	ok, s, p = m.Map((256000 & 0xFFFF) + 2, 1)
	expect := uint16((256000) & 0xFFFF)
	if !ok || s != expect || p != 1 {
		t.Errorf("Expected %v, 1, got %v, %v, %v", expect, ok, s, p)
	}
}

func TestWraparoundDrop(t *testing.T) {
	m := Map{}

	ok, s, p := m.Map(0, 0)
	if !ok || s != 0 || p != 0 {
		t.Errorf("Expected 0, 0, got %v, %v, %v", ok, s, p)
	}

	ok, s, p = m.Map(1, 0)
	if !ok || s != 1 || p != 0 {
		t.Errorf("Expected 1, 0, got %v, %v, %v", ok, s, p)
	}

	ok = m.Drop(2, 1)
	if !ok || m.pidDelta != 1 {
		t.Errorf("Expected 1, got %v, %v", ok, m.pidDelta)
	}

	ok = m.Drop(3, 1)
	if !ok || m.pidDelta != 1 {
		t.Errorf("Expected 1, got %v, %v", ok, m.pidDelta)
	}

	for i := 4; i < 256000; i+= 3 {
		ok, s, p = m.Map(uint16(i), uint16((i/2) & 0x7FFF))
		if !ok || s != uint16((i-1)/3*2) || p != 1 {
			t.Errorf("Expected %v, %v, got %v, %v, %v",
				uint16((i-1)/3*2), 1, ok, s, p)
		}
		ok, s, p = m.Map(uint16(i + 1), uint16((i/2) & 0x7FFF))
		if !ok || s != uint16((i-1)/3*2 + 1) || p != 1 {
			t.Errorf("Expected %v, %v, got %v, %v, %v",
				uint16((i-1)/3*2 + 1), 1, ok, s, p)
		}
		ok = m.Drop(uint16(i + 2), uint16((i/2) & 0x7FFF))
		if !ok {
			t.Errorf("Expected ok")
		}
	}

	ok, s, p = m.Map((256000 & 0xFFFF) + 4, 0)
	expect := uint16(((256000 - 1)/3*2 + 4) & 0xFFFF)
	if !ok || s != expect || p != 1 {
		t.Errorf("Expected %v, 1, got %v, %v, %v", expect, ok, s, p)
	}
}

func TestReset(t *testing.T) {
	m := Map{}

	ok, s, p := m.Map(42, 1001)
	if !ok || s != 42 || p != 0 {
		t.Errorf("Expected 42, 0, got %v, %v, %v", ok, s, p)
	}

	ok = m.Drop(43, 1001)
	if !ok || m.pidDelta != 0 {
		t.Errorf("Expected 0, got %v, %v", ok, m.pidDelta)
	}

	ok, s, p = m.Map(44, 1001)
	if !ok || s != 43 || p != 0 {
		t.Errorf("Expected 43, 0, got %v, %v, %v", ok, s, p)
	}

	ok, s, p = m.Map(40000, 2001)
	if !ok || s != 40000 || p != 0 {
		t.Errorf("Expected 32000, 0, got %v, %v, %v", ok, s, p)
	}

	if m.delta != 0 || m.entries != nil {
		t.Errorf("Expected reset")
	}

	ok, s, p = m.Map(40001, 2001)
	if !ok || s != 40001 || p != 0 {
		t.Errorf("Expected 32001, 0, got %v, %v, %v", ok, s, p)
	}
}
