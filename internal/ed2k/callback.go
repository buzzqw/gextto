package ed2k

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"
)

// SendDirectCallback asks a firewalled (LowID) source to call us back on our
// TCP port. The packet is the eMule UDP OP_DIRECTCALLBACKREQ:
// protocol 0xC5, opcode 0x95, TCP port, user hash and connection options.
// The source then opens a normal client connection to our listener.
func (c *Client) SendDirectCallback(ctx context.Context, source PeerSource, ourPort uint16) error {
	if source.IP == nil || source.UDPPort == 0 {
		return errors.New("ed2k: source has no UDP port for a direct callback")
	}
	if ourPort == 0 {
		return errors.New("ed2k: invalid callback TCP port")
	}
	if err := c.Start(ctx); err != nil {
		return err
	}
	c.mu.Lock()
	conn := c.conn
	hash := c.userHashValue
	c.mu.Unlock()
	if conn == nil {
		return errors.New("ed2k: Kad socket is not open")
	}
	ip := source.IP.To4()
	if ip == nil {
		return errors.New("ed2k: direct callback requires IPv4")
	}
	packet := make([]byte, 2+2+16+1)
	packet[0] = 0xC5
	packet[1] = 0x95
	binary.LittleEndian.PutUint16(packet[2:], ourPort)
	copy(packet[4:], hash[:])
	packet[20] = 0 // no obfuscation
	addr := &net.UDPAddr{IP: ip, Port: int(source.UDPPort)}
	if _, err := conn.WriteToUDP(packet, addr); err != nil {
		return fmt.Errorf("ed2k: direct callback to %s: %w", addr, err)
	}
	return nil
}

// SendServerCallback asks an eD2k server to tell a LowID source to connect to
// us. The source is identified by the ID the server assigned to it; the server
// sends OP_CALLBACKREQUESTED to the source with our IP/TCP port.
func (c *Client) SendServerCallback(ctx context.Context, source PeerSource) error {
	if !source.LowID || source.ClientID == 0 {
		return errors.New("ed2k: source has no server callback ID")
	}
	if source.ServerIP == nil || source.ServerPort == 0 {
		return errors.New("ed2k: source has no server for the callback")
	}
	address := net.JoinHostPort(source.ServerIP.String(), strconv.Itoa(int(source.ServerPort)))
	if c.serverBlocked(ctx, address) {
		return fmt.Errorf("ed2k: callback server %s blocked by the IP filter", address)
	}
	dialer := &net.Dialer{Timeout: 8 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("ed2k: callback server %s: %w", address, err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	}
	if err := c.serverLogin(conn); err != nil {
		return fmt.Errorf("ed2k: callback server login: %w", err)
	}
	var payload [4]byte
	binary.LittleEndian.PutUint32(payload[:], source.ClientID)
	if err := writeServerPacket(conn, OpCallbackReq, payload[:]); err != nil {
		return fmt.Errorf("ed2k: callback request: %w", err)
	}
	for {
		opcode, _, err := readServerPacket(conn)
		if err != nil {
			if isTimeout(err) {
				// Some servers do not acknowledge a successful request; the
				// source can already be connecting to us.
				return nil
			}
			return fmt.Errorf("ed2k: callback answer: %w", err)
		}
		switch opcode {
		case OpCallbackOK:
			return nil
		case OpCallbackFail:
			return errors.New("ed2k: server rejected the callback")
		}
	}
}

// CanCallback reports whether our TCP port is reachable (HighID). A direct
// callback advertises that port, so it is only useful when it is open.
func (c *Client) CanCallback() bool {
	stats := c.Stats()
	return stats.Reachability == "open" || (stats.ServerIDSet && !stats.LowID)
}

// CallbackIdentity returns the user hash and TCP port advertised in a direct
// callback request.
func (c *Client) CallbackIdentity() (ID, uint16) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.userHashValue, uint16(c.cfg.TCPPort)
}

// SendBuddyCallback asks a LowID source's buddy to tell the source to connect
// to us. Payload: buddy ID, file hash and our TCP port (KADEMLIA_CALLBACK_REQ).
func (c *Client) SendBuddyCallback(ctx context.Context, source PeerSource, fileHash ID, ourPort uint16) error {
	if source.BuddyID == (ID{}) || source.BuddyIP == nil || source.BuddyPort == 0 {
		return errors.New("ed2k: source has no buddy")
	}
	if ourPort == 0 {
		return errors.New("ed2k: invalid callback TCP port")
	}
	if err := c.Start(ctx); err != nil {
		return err
	}
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return errors.New("ed2k: Kad socket is not open")
	}
	payload := make([]byte, 0, 34)
	payload = append(payload, source.BuddyID[:]...)
	payload = append(payload, fileHash[:]...)
	var port [2]byte
	binary.LittleEndian.PutUint16(port[:], ourPort)
	payload = append(payload, port[:]...)
	addr := &net.UDPAddr{IP: source.BuddyIP, Port: int(source.BuddyPort)}
	if _, err := conn.WriteToUDP(kadPacket(KadCallbackReq, payload), addr); err != nil {
		return fmt.Errorf("ed2k: buddy callback to %s: %w", addr, err)
	}
	return nil
}
