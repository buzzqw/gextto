package ed2k

import (
	"os"
	"testing"
)

func TestLiveNATMapping(t *testing.T) {
	if os.Getenv("ED2K_LIVE_NAT") == "" {
		t.Skip("set ED2K_LIVE_NAT=1 to probe the local router")
	}
	for _, name := range []string{"natpmp", "pcp"} {
		var result UPnPResult
		var err error
		if name == "natpmp" {
			result, err = mapNATPMP(4672, 4662)
		} else {
			result, err = mapPCP(t.Context(), 4672, 4662)
		}
		t.Logf("%s: %+v err=%v", name, result, err)
	}
}
