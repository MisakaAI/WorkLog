package main

import (
	"encoding/binary"
	"errors"
)

// MIB_TCPTABLE_OWNER_PID: DWORD count followed by 24-byte rows.
func parseTCPTable(table []byte, pid uint32) (uint16, error) {
	if len(table) < 4 {
		return 0, errors.New("truncated TCP table")
	}
	count := uint64(binary.LittleEndian.Uint32(table[:4]))
	if count > uint64((len(table)-4)/24) {
		return 0, errors.New("truncated TCP rows")
	}
	var port uint16
	for i := 0; i < int(count); i++ {
		row := table[4+i*24 : 4+(i+1)*24]
		if binary.LittleEndian.Uint32(row[20:24]) != pid {
			continue
		}
		if binary.LittleEndian.Uint32(row[:4]) != 2 || // MIB_TCP_STATE_LISTEN
			row[4] != 127 || row[5] != 0 || row[6] != 0 || row[7] != 1 {
			return 0, errors.New("backend listener is not IPv4 loopback")
		}
		p := binary.BigEndian.Uint16(row[8:10])
		if p == 0 || port != 0 {
			return 0, errors.New("invalid or ambiguous backend listener")
		}
		port = p
	}
	return port, nil
}
