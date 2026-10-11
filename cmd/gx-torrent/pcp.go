package main

// Minimal PCP (RFC 6887) MAP client, used to open the peer port on a router
// that speaks PCP but not (or not reliably) NAT-PMP. It is a third method next
// to UPnP and NAT-PMP; a MAP request is stateless, so removing the mapping is
// just another MAP with lifetime 0. Only the MAP opcode is implemented: PEER
// (which asks the server to also carry traffic) is not needed here.

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"time"
)

const (
	pcpVersion   = 2
	pcpPort      = 5351
	pcpOpcodeMap = 1
	pcpProtoTCP  = 6
	pcpProtoUDP  = 17

	pcpRequestLen  = 60 // 24-byte header + 36-byte MAP data
	pcpResponseMin = 60
)

// pcpMap asks the PCP server at gateway:port to map an internal port to an
// external one. It returns the assigned external port and IP (which the server
// echoes only on success). lifetime 0 removes the mapping.
func pcpMap(ctx context.Context, gateway net.IP, port int, proto byte, internalPort uint16, lifetime uint32) (uint16, net.IP, error) {
	req := make([]byte, pcpRequestLen)
	req[0] = pcpVersion
	req[1] = pcpOpcodeMap
	binary.BigEndian.PutUint32(req[4:8], lifetime)
	// PCP Client's IP Address = 0: "use the source address of the packet".
	nonce := req[24:36]
	if _, err := rand.Read(nonce); err != nil {
		return 0, nil, err
	}
	req[36] = proto
	binary.BigEndian.PutUint16(req[40:42], internalPort)
	// Suggested external port = 0 and external IP = 0: let the server choose.

	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "udp", net.JoinHostPort(gateway.String(), fmt.Sprint(port)))
	if err != nil {
		return 0, nil, err
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	}
	if _, err := conn.Write(req); err != nil {
		return 0, nil, err
	}
	resp := make([]byte, 1100)
	n, err := conn.Read(resp)
	if err != nil {
		return 0, nil, err
	}
	if n < pcpResponseMin {
		return 0, nil, fmt.Errorf("PCP response too short (%d bytes)", n)
	}
	if resp[0] != pcpVersion {
		return 0, nil, fmt.Errorf("PCP unsupported version %d", resp[0])
	}
	if resp[1]&0x7f != pcpOpcodeMap || resp[1]&0x80 == 0 {
		return 0, nil, fmt.Errorf("PCP unexpected opcode %d", resp[1])
	}
	if string(resp[24:36]) != string(nonce) {
		return 0, nil, errors.New("PCP nonce mismatch")
	}
	if code := resp[3]; code != 0 {
		return 0, nil, fmt.Errorf("PCP result code %d", code)
	}
	extPort := binary.BigEndian.Uint16(resp[42:44])
	extIP := net.IP(append([]byte(nil), resp[44:60]...))
	if v4 := extIP.To4(); v4 != nil {
		extIP = v4
	}
	return extPort, extIP, nil
}
