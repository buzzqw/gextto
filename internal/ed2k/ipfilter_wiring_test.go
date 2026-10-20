package ed2k

import (
	"context"
	"net"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/ed2k/ipfilter"
)

func testFilter(t *testing.T) *ipfilter.Filter {
	t.Helper()
	filter, err := ipfilter.Parse(strings.NewReader("001.002.003.000 - 001.002.003.255 , 000 , blocked\n"))
	if err != nil {
		t.Fatal(err)
	}
	return filter
}

func TestRoutingTableSkipsBlockedContacts(t *testing.T) {
	table := newRoutingTable(8, testFilter(t))
	self := ID{1}
	blocked := Contact{ID: ID{2}, IP: net.ParseIP("1.2.3.4"), UDPPort: 4672, Version: 4}
	if added := table.add(blocked, self); added || table.len() != 0 {
		t.Fatal("blocked Kad contact was added")
	}
	allowed := Contact{ID: ID{3}, IP: net.ParseIP("9.9.9.9"), UDPPort: 4672, Version: 4}
	if added := table.add(allowed, self); !added || table.len() != 1 {
		t.Fatal("allowed Kad contact was not added")
	}
}

func TestFilterSourcesDropsBlockedPeers(t *testing.T) {
	client := &Client{cfg: Config{IPFilter: testFilter(t)}}
	sources := []PeerSource{
		{IP: net.ParseIP("1.2.3.4"), TCPPort: 4662, BuddyIP: net.ParseIP("9.9.9.9"), BuddyPort: 4662},
		{IP: net.ParseIP("9.9.9.9"), TCPPort: 4662, BuddyIP: net.ParseIP("1.2.3.4"), BuddyID: ID{7}, BuddyPort: 4662},
	}
	filtered := client.filterSources(sources)
	if len(filtered) != 1 || !filtered[0].IP.Equal(net.ParseIP("9.9.9.9")) {
		t.Fatalf("unexpected sources: %+v", filtered)
	}
	if filtered[0].BuddyIP != nil || filtered[0].BuddyID != (ID{}) || filtered[0].BuddyPort != 0 {
		t.Fatal("blocked buddy was kept")
	}
}

func TestServerBlocked(t *testing.T) {
	client := &Client{cfg: Config{IPFilter: testFilter(t)}}
	if !client.serverBlocked(context.Background(), "1.2.3.4:5000") {
		t.Fatal("blocked server address allowed")
	}
	if client.serverBlocked(context.Background(), "9.9.9.9:5000") {
		t.Fatal("allowed server address blocked")
	}
	if client.serverBlocked(context.Background(), "127.0.0.1:5000") {
		t.Fatal("loopback address blocked")
	}
}
