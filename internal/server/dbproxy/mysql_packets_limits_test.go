package dbproxy

import (
	"net"
	"testing"
)

func TestReadMySQLPacketReadsFragmentedHeaderAndBody(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, fragment := range [][]byte{{3}, {0, 0}, {7}, {1}, {2, 3}} {
			_, _ = server.Write(fragment)
		}
	}()

	packet, err := readMySQLPacket(client)
	if err != nil {
		t.Fatal(err)
	}
	if packet.seq != 7 || string(packet.payload) != "\x01\x02\x03" {
		t.Fatalf("packet = seq %d payload %x", packet.seq, packet.payload)
	}
	<-done
}

func TestReadMySQLPacketRejectsOversizedPreAuthPacketBeforeAllocation(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	go func() { _, _ = server.Write([]byte{0xff, 0xff, 0xff, 1}) }() // protocol-maximum declaration

	if _, err := readMySQLPacket(client); err == nil {
		t.Fatal("expected oversized packet declaration to be rejected")
	}
}
