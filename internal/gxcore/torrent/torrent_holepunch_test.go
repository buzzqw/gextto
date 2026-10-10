package torrent

import (
	"net"
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/peerprotocol"
)

func holepunchTCPAddr(ip string, port int) *net.TCPAddr {
	return &net.TCPAddr{IP: net.ParseIP(ip), Port: port}
}

// The relay sends connect to both the initiator and the target when it is
// connected to the target and the target supports the extension.
func TestPlanHolepunchRendezvousRelays(t *testing.T) {
	sender := holepunchTCPAddr("1.2.3.4", 1000)
	rendezvous := peerprotocol.HolepunchMessage{
		Type: peerprotocol.HolepunchRendezvous,
		Addr: net.IPv4(5, 6, 7, 8),
		Port: 2000,
	}
	relays := []holepunchRelay{
		{Addr: holepunchTCPAddr("9.9.9.9", 3000), Supports: true}, // unrelated peer
		{Addr: holepunchTCPAddr("5.6.7.8", 2000), Supports: true}, // the target
	}
	actions := planHolepunchRendezvous(sender, rendezvous, relays)
	if len(actions) != 2 {
		t.Fatalf("actions = %+v, want connect to sender and target", actions)
	}
	toSender := actions[0]
	if toSender.Target != -1 || toSender.Msg.Type != peerprotocol.HolepunchConnect {
		t.Fatalf("first action %+v, want connect to sender", toSender)
	}
	if !toSender.Msg.Addr.Equal(net.IPv4(5, 6, 7, 8)) || toSender.Msg.Port != 2000 {
		t.Fatalf("sender connect endpoint = %v:%d, want the target", toSender.Msg.Addr, toSender.Msg.Port)
	}
	toTarget := actions[1]
	if toTarget.Target != 1 || toTarget.Msg.Type != peerprotocol.HolepunchConnect {
		t.Fatalf("second action %+v, want connect to the target peer", toTarget)
	}
	if !toTarget.Msg.Addr.Equal(net.IPv4(1, 2, 3, 4)) || toTarget.Msg.Port != 1000 {
		t.Fatalf("target connect endpoint = %v:%d, want the sender", toTarget.Msg.Addr, toTarget.Msg.Port)
	}
}

func TestPlanHolepunchRendezvousErrors(t *testing.T) {
	sender := holepunchTCPAddr("1.2.3.4", 1000)
	target := peerprotocol.HolepunchMessage{Addr: net.IPv4(5, 6, 7, 8), Port: 2000}

	// No connected peer matches the requested target.
	actions := planHolepunchRendezvous(sender, target, []holepunchRelay{{Addr: holepunchTCPAddr("9.9.9.9", 3000), Supports: true}})
	assertHolepunchError(t, actions, peerprotocol.HolepunchNotConnected)

	// Connected to the target but it does not support the extension.
	actions = planHolepunchRendezvous(sender, target, []holepunchRelay{{Addr: holepunchTCPAddr("5.6.7.8", 2000)}})
	assertHolepunchError(t, actions, peerprotocol.HolepunchNoSupport)

	// The requested endpoint is the relay's own connection to the sender.
	self := sender
	actions = planHolepunchRendezvous(sender, peerprotocol.HolepunchMessage{Addr: self.IP, Port: uint16(self.Port)}, []holepunchRelay{{Addr: self, Supports: true}})
	assertHolepunchError(t, actions, peerprotocol.HolepunchNoSelf)

	// A rendezvous without a usable target address.
	actions = planHolepunchRendezvous(sender, peerprotocol.HolepunchMessage{}, nil)
	assertHolepunchError(t, actions, peerprotocol.HolepunchNoSuchPeer)
}

func assertHolepunchError(t *testing.T, actions []holepunchRelayMsg, want peerprotocol.HolepunchErrCode) {
	t.Helper()
	if len(actions) != 1 || actions[0].Target != -1 {
		t.Fatalf("actions = %+v, want a single error to the sender", actions)
	}
	msg := actions[0].Msg
	if msg.Type != peerprotocol.HolepunchError || msg.ErrCode != want {
		t.Fatalf("error = %+v, want code %d", msg, want)
	}
}
