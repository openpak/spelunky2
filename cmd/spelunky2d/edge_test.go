package main

import (
	"bytes"
	"log"
	"net"
	"testing"
	"time"
)

func TestEdgeProbeLogged(t *testing.T) {
	var logs bytes.Buffer
	old := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(old) })

	go serveEdgeProbes("127.0.0.1:18281")
	time.Sleep(300 * time.Millisecond)
	c, err := net.Dial("udp", "127.0.0.1:18281")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Write([]byte{7, 0x24}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if !bytes.Contains(logs.Bytes(), []byte("edge=7")) {
		t.Errorf("probe not logged: %q", logs.String())
	}
}
