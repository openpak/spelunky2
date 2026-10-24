package main

import (
	"encoding/binary"
	"net"
	"testing"
	"time"
)

func TestProbeAnswersOneByte(t *testing.T) {
	go serveProbePort("127.0.0.1:18281")
	time.Sleep(200 * time.Millisecond)
	c, err := net.Dial("udp", "127.0.0.1:18281")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_, _ = c.Write([]byte{7, 0x14})
	rep := make([]byte, 32)
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := c.Read(rep)
	if err != nil || n != 1 || rep[0] != 7 {
		t.Fatalf("reply %x (%v), want the single byte 07", rep[:n], err)
	}
}

func record(name string, pid uint64) []byte {
	r := make([]byte, 0x17+len(name))
	r[0] = byte(len(name))
	binary.LittleEndian.PutUint64(r[8:], pid)
	r[0x15] = 0x20 << 2
	copy(r[0x17:], name)
	return r
}

func statePacket(lobbyField uint64, seq uint32, rec []byte) []byte {
	st := make([]byte, stateLen)
	binary.LittleEndian.PutUint64(st, lobbyField)
	binary.LittleEndian.PutUint32(st[8:], seq)
	for i := 17; i < 49; i++ {
		st[i] = 0xff
	}
	st[17+3] = 9 // edge-03 answers fastest
	return append(st, rec...)
}

func TestLobbyCreateJoinAndHub(t *testing.T) {
	w := newWorld()
	h := w.handleState(statePacket(lobbyCreate, 1, record("host", 11)))
	id := binary.LittleEndian.Uint64(h[0x10:])
	if id == 0 || h[0x20] != 0 || binary.LittleEndian.Uint32(h[0x18:]) != 1 {
		t.Fatalf("create reply %x", h[:0x48])
	}
	if e := binary.LittleEndian.Uint64(h[0:]); e != 4 {
		t.Fatalf("edge %d, want 4 (edge-03, 1-based)", e)
	}
	hs := binary.LittleEndian.Uint64(h[8:])
	if hs == 0 {
		t.Fatal("no session for the host")
	}
	if len(h) != 0x48+0x17+4+3 {
		t.Fatalf("reply len %d", len(h))
	}

	j := w.handleState(statePacket(id, 1, record("join", 22)))
	if binary.LittleEndian.Uint64(j[0x10:]) != id || j[0x20] != 1 {
		t.Fatalf("join reply %x", j[:0x48])
	}
	js := binary.LittleEndian.Uint64(j[8:])

	if r := w.handleState(statePacket(0x1234560, 1, record("lost", 33))); binary.LittleEndian.Uint64(r[0x10:]) != 0 {
		t.Fatal("join of a missing lobby should leave the player idle")
	}

	ha := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 1000}
	ja := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 2), Port: 2000}
	sess := func(s uint64, acks [4]uint32, off int, data []byte) []byte {
		p := make([]byte, sessionHeader)
		binary.LittleEndian.PutUint64(p, s)
		for i, a := range acks {
			binary.LittleEndian.PutUint32(p[8+4*i:], a)
		}
		binary.LittleEndian.PutUint32(p[24:], uint32(off)<<8|uint32(len(data)))
		return append(p, data...)
	}
	_ = w.handleSession(sess(js, [4]uint32{}, 0, nil), ja)
	out := w.handleSession(sess(hs, [4]uint32{}, 0, []byte{0x00, 0x11}), ha)
	if len(out) != 2 {
		t.Fatalf("want an ack to the host and a push to the joiner, got %d packets", len(out))
	}
	if a := out[0].pkt; a[1] != 2 || a[0]>>4 != 3 {
		t.Fatalf("host ack %x: want 2 bytes held, slots 0+1 present", a)
	}
	p := out[1].pkt
	if out[1].to != ja || p[0]&0x0f != 1 || binary.LittleEndian.Uint32(p[4:]) != 2 || p[8] != 0x00 || p[9] != 0x11 {
		t.Fatalf("push to joiner %x", p)
	}
	// A resend of the same bytes adds nothing and pushes nothing.
	if out := w.handleSession(sess(hs, [4]uint32{}, 0, []byte{0x00, 0x11}), ha); len(out) != 1 {
		t.Fatalf("resend pushed %d packets", len(out))
	}
	// Once the joiner holds the host's 2 bytes, its answers carry no chunk.
	out = w.handleSession(sess(js, [4]uint32{2, 0, 0, 0}, 0, nil), ja)
	if out[0].pkt[0]&0x0f != 0 || len(out[0].pkt) != 4 {
		t.Fatalf("joiner answer %x, want header only", out[0].pkt)
	}
}
