package dbproxy

import (
	"bufio"
	"bytes"
	"testing"
)

func FuzzRedisRESPFrameLength(f *testing.F) {
	seeds := [][]byte{
		[]byte("+OK\r\n"),
		[]byte("$5\r\nvalue\r\n"),
		[]byte("*2\r\n$3\r\nGET\r\n$3\r\nkey\r\n"),
		[]byte("%2\r\n+map\r\n~2\r\n:1\r\n:2\r\n+null\r\n_\r\n"),
		[]byte("|1\r\n+ttl\r\n:10\r\n>2\r\n+invalidate\r\n$3\r\nkey\r\n"),
		[]byte("*2\r\n*2\r\n:1\r\n:2\r\n*1\r\n#t\r\n"),
		[]byte("$5\r\nval"),
		[]byte("*2\r\n$3\r\nGET\r\n$999999999999999999999\r\n"),
		[]byte("%999999999999999999999\r\n"),
		[]byte("!4\r\nERR\n"),
		[]byte("#x\r\n"),
		[]byte("|1\r\n+k\r\n+v\r\n"),
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		length, status := redisRESPFrameLength(data)
		switch status {
		case redisRESPComplete:
			if length <= 0 || length > len(data) {
				t.Fatalf("complete frame length = %d for %d bytes", length, len(data))
			}
			if exactLength, exactStatus := redisRESPFrameLength(data[:length]); exactStatus != redisRESPComplete || exactLength != length {
				t.Fatalf("exact frame reparsed as status=%v length=%d, want complete %d", exactStatus, exactLength, length)
			}
		case redisRESPIncomplete, redisRESPMalformed, redisRESPLimitExceeded:
		default:
			t.Fatalf("unknown RESP status %d", status)
		}
	})
}

func FuzzRedisObserverClientFrames(f *testing.F) {
	seeds := [][]byte{
		redisObserverTestCommand("GET", "key"),
		[]byte("*2\r\n$3\r\nGET\r\n$-1\r\n"),
		[]byte("*2\r\n$3\r\nGET\r\n$-2\r\n"),
		[]byte("*2\r\n$3\r\nGET\r\n$999999999999999999999\r\n"),
		[]byte("*2\r\n$3\r\nGET\r\n$3\r\nke"),
		[]byte("*0\r\n"),
		[]byte("*1\r\n+PING\r\n"),
	}
	for _, seed := range seeds {
		f.Add(seed, uint8(1))
	}

	f.Fuzz(func(t *testing.T, data []byte, chunkSize uint8) {
		observer := &redisObserver{}
		size := int(chunkSize) + 1
		for offset := 0; offset < len(data); {
			end := offset + size
			if end > len(data) {
				end = len(data)
			}
			observer.ObserveClientBytes(data[offset:end])
			offset = end
		}
	})
}

func FuzzRedisAuthenticationCommandParser(f *testing.F) {
	seeds := [][]byte{
		redisObserverTestCommand("AUTH", "R000100001", "password"),
		redisObserverTestCommand("HELLO", "3", "AUTH", "R000100001", "password"),
		[]byte("*2\r\n$4\r\nAUTH\r\n$-1\r\n"),
		[]byte("*2\r\n$4\r\nAUTH\r\n$-2\r\n"),
		[]byte("*2\r\n$4\r\nAUTH\r\n$999999999999999999999\r\n"),
		[]byte("*3\r\n$4\r\nAUTH\r\n$4\r\nuser\r\n$8\r\npassword"),
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		reader := bufio.NewReader(bytes.NewReader(data))
		firstLine, err := readRESPAuthLine(reader)
		if err != nil {
			return
		}
		_, _, raw, _ := readRESPAuthCommand(reader, firstLine, true)
		if len(raw) > maxRESPAuthCommandLen {
			t.Fatalf("authentication parser returned %d raw bytes", len(raw))
		}
	})
}
