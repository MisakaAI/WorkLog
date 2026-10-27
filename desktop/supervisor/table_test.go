package main

import (
	"encoding/binary"
	"testing"
)

func tcpTable(pid uint32, port uint16) []byte {
	table := make([]byte, 4+24)
	binary.LittleEndian.PutUint32(table[:4], 1)
	row := table[4:]
	binary.LittleEndian.PutUint32(row[:4], 2)
	copy(row[4:8], []byte{127, 0, 0, 1})
	binary.BigEndian.PutUint16(row[8:10], port)
	binary.LittleEndian.PutUint32(row[20:24], pid)
	return table
}

func TestPortBelongsToBackend(t *testing.T) {
	for _, p := range []uint16{1, 12450, 49152, 65535} {
		port, err := parseTCPTable(tcpTable(42, p), 42)
		if err != nil || port != p {
			t.Fatalf("port=%d err=%v", port, err)
		}
	}
	if port, err := parseTCPTable(tcpTable(41, 50000), 42); err != nil || port != 0 {
		t.Fatalf("another process matched: port=%d err=%v", port, err)
	}
	if port, err := parseTCPTable(make([]byte, 4), 42); err != nil || port != 0 {
		t.Fatalf("empty table: port=%d err=%v", port, err)
	}
}

func TestRejectInvalidListeners(t *testing.T) {
	for _, mutate := range []func([]byte) []byte{
		func(b []byte) []byte { return b[:3] },
		func(b []byte) []byte { return b[:len(b)-1] },
		func(b []byte) []byte { b[8] = 0; return b }, // public interface
		func(b []byte) []byte { b[4] = 5; return b }, // not LISTEN
		func(b []byte) []byte { b[12], b[13] = 0, 0; return b },
		func(b []byte) []byte {
			b = append(b, b[4:]...)
			binary.LittleEndian.PutUint32(b[:4], 2)
			return b
		},
	} {
		if _, err := parseTCPTable(mutate(tcpTable(42, 55001)), 42); err == nil {
			t.Fatal("accepted invalid listener table")
		}
	}
}
