package ed2k

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
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
