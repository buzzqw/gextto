package gextto

// local_process.go samples the resource usage of a process running on this
// machine, for the header badge of aMule. aMule is an external daemon: when it
// runs elsewhere there is nothing to read, and the badge is simply absent.

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
)

// localProcessUsage is the summed CPU and memory of one or more processes.
type localProcessUsage struct {
	RSSBytes   uint64
	CPUPercent *float64
	PIDs       []int
}

// localProcessSample remembers the previous CPU ticks of a process group, so a
// percentage can be computed from the delta, and the percentage it produced.
type localProcessSample struct {
	ticks   uint64
	at      time.Time
	percent *float64
}

// localProcessMinWindow is the shortest interval a CPU percentage is measured
// over. Several tabs refresh the header within moments of each other: a closer
// call reuses the last percentage instead of a noisy one from a tiny window.
const localProcessMinWindow = 2 * time.Second

// localProcessSampleTTL drops the samples of process groups no longer read
// (a restarted aMule or gx-torrent has new pids, so a new key).
const localProcessSampleTTL = 5 * time.Minute

// localProcessPIDsTTL is how long the pids found for a name are reused: the
// header refreshes every few seconds and walking /proc each time is wasted.
const localProcessPIDsTTL = 15 * time.Second

type localProcessPIDsEntry struct {
	pids []int
	at   time.Time
}

var (
	localProcessMu      sync.Mutex
	localProcessSamples = map[string]localProcessSample{}

	localProcessPIDsMu    sync.Mutex
	localProcessPIDsCache = map[string]localProcessPIDsEntry{}
)

// localProcessUsageForName sums every live process whose name (the kernel
// `comm`, at most 15 characters) matches. It reports false when none runs.
func localProcessUsageForName(name string) (localProcessUsage, bool) {
	pids := localProcessPIDsCached(name)
	if len(pids) == 0 {
		return localProcessUsage{}, false
	}
	if usage, ok := localProcessUsageForPIDs(pids); ok {
		return usage, true
	}
	// The cached pids are gone (the process restarted): look again once.
	localProcessForgetPIDs(name)
	pids = localProcessPIDsCached(name)
	if len(pids) == 0 {
		return localProcessUsage{}, false
	}
	return localProcessUsageForPIDs(pids)
}

// localProcessPIDsCached is localProcessPIDs reused for localProcessPIDsTTL.
func localProcessPIDsCached(name string) []int {
	localProcessPIDsMu.Lock()
	defer localProcessPIDsMu.Unlock()
	if entry, ok := localProcessPIDsCache[name]; ok && time.Since(entry.at) < localProcessPIDsTTL {
		return entry.pids
	}
	pids := localProcessPIDs(name)
	localProcessPIDsCache[name] = localProcessPIDsEntry{pids: pids, at: time.Now()}
	return pids
}

// localProcessForgetPIDs drops the cached pids of a name.
func localProcessForgetPIDs(name string) {
	localProcessPIDsMu.Lock()
	delete(localProcessPIDsCache, name)
	localProcessPIDsMu.Unlock()
}

// localProcessPIDs lists the local processes whose kernel name matches.
func localProcessPIDs(name string) []int {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	pids := make([]int, 0, 4)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		raw, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "comm"))
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(raw)) == name {
			pids = append(pids, pid)
		}
	}
	return pids
}

// processRunsInContainer reports whether a process belongs to a container, by
// looking at its cgroup. A container's filesystem is not this machine's, so
// paths inside it must not be checked or created from here.
func processRunsInContainer(pid int) bool {
	if pid <= 0 {
		return false
	}
	raw, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cgroup"))
	if err != nil {
		return false
	}
	value := strings.ToLower(string(raw))
	for _, marker := range []string{"docker", "kubepods", "containerd", "libpod", "lxc"} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

// localProcessUsageForPIDs sums the resident memory and the CPU ticks of the
// given processes. The CPU percentage is measured against the previous sample
// of the same name (or pid), so the first call returns memory only.
func localProcessUsageForPIDs(pids []int) (localProcessUsage, bool) {
	if len(pids) == 0 {
		return localProcessUsage{}, false
	}
	key := localProcessKey(pids)
	var rss uint64
	var ticks uint64
	found := 0
	for _, pid := range pids {
		statm, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "statm"))
		if err != nil {
			continue
		}
		fields := strings.Fields(string(statm))
		if len(fields) < 2 {
			continue
		}
		pages, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		ticks += localProcessTicks(pid)
		rss = healthSatAdd(rss, healthSatMul(pages, healthPageSize()))
		found++
	}
	if found == 0 {
		return localProcessUsage{}, false
	}

	now := time.Now()
	clockTicks := float64(healthClockTicks())
	if clockTicks < 1 {
		clockTicks = 1
	}
	var percent *float64
	localProcessMu.Lock()
	previous, seen := localProcessSamples[key]
	if seen && now.Sub(previous.at) < localProcessMinWindow {
		// Too close to the last sample: keep it and its percentage.
		percent = previous.percent
	} else {
		if seen {
			value := float64(healthSatSub(ticks, previous.ticks)) / clockTicks / now.Sub(previous.at).Seconds() * 100.0
			percent = &value
		}
		localProcessSamples[key] = localProcessSample{ticks: ticks, at: now, percent: percent}
		for other, sample := range localProcessSamples {
			if now.Sub(sample.at) > localProcessSampleTTL {
				delete(localProcessSamples, other)
			}
		}
	}
	localProcessMu.Unlock()
	return localProcessUsage{RSSBytes: rss, CPUPercent: percent, PIDs: pids}, true
}

// localProcessKey names a process group in the sample cache: a single pid
// keeps its own entry, a group is keyed by its sorted pids.
func localProcessKey(pids []int) string {
	if len(pids) == 1 {
		return "pid:" + strconv.Itoa(pids[0])
	}
	parts := make([]string, 0, len(pids))
	for _, pid := range pids {
		parts = append(parts, strconv.Itoa(pid))
	}
	return "pids:" + strings.Join(parts, ",")
}

// localProcessTicks reads utime+stime of one process, 0 when unreadable.
func localProcessTicks(pid int) uint64 {
	raw, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0
	}
	text := string(raw)
	end := strings.LastIndex(text, ")")
	if end < 0 || end+2 > len(text) {
		return 0
	}
	fields := strings.Fields(text[end+2:])
	if len(fields) < 13 {
		return 0
	}
	utime, err := strconv.ParseUint(fields[11], 10, 64)
	if err != nil {
		return 0
	}
	stime, err := strconv.ParseUint(fields[12], 10, 64)
	if err != nil {
		return 0
	}
	return healthSatAdd(utime, stime)
}

// localProcessLoadLabel renders "CPU% · RAM" for a header badge. The CPU part
// is a dash until a second sample makes the percentage meaningful.
func localProcessLoadLabel(usage localProcessUsage) string {
	cpu := "—"
	if usage.CPUPercent != nil {
		cpu = strconv.FormatFloat(*usage.CPUPercent, 'f', 1, 64) + "%"
	}
	return cpu + " · " + logging.HumanBytesI64(saturatingInt64(usage.RSSBytes))
}

// hostIsLocal reports whether a configured host names this machine, so a local
// process can be sampled. A hostname is compared with the machine name only:
// no DNS lookup on every render.
func hostIsLocal(host string) bool {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "" || host == "localhost" || host == "::1" {
		return true
	}
	if ip := parseIPAddress(host); ip != "" {
		if ip == "127.0.0.1" || strings.HasPrefix(ip, "127.") {
			return true
		}
		return localAddresses()[ip]
	}
	if name, err := os.Hostname(); err == nil {
		if strings.EqualFold(host, name) || strings.EqualFold(host, name+".local") {
			return true
		}
	}
	return false
}

// parseIPAddress normalises an IPv4/IPv6 literal, returning "" for a name.
func parseIPAddress(value string) string {
	value = strings.Trim(value, "[]")
	if value == "" {
		return ""
	}
	parsed := net.ParseIP(value)
	if parsed == nil {
		return ""
	}
	return parsed.String()
}

// localAddresses lists this machine's interface addresses, cached briefly: the
// chrome renders on every page and every SSE tick.
var (
	localAddressesMu   sync.Mutex
	localAddressesAt   time.Time
	localAddressesSeen map[string]bool
)

const localAddressesTTL = 60 * time.Second

func localAddresses() map[string]bool {
	localAddressesMu.Lock()
	defer localAddressesMu.Unlock()
	if localAddressesSeen != nil && time.Since(localAddressesAt) < localAddressesTTL {
		return localAddressesSeen
	}
	seen := map[string]bool{}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, addr := range addrs {
			value := addr.String()
			if index := strings.IndexByte(value, '/'); index >= 0 {
				value = value[:index]
			}
			if ip := parseIPAddress(value); ip != "" {
				seen[ip] = true
			}
		}
	}
	localAddressesSeen = seen
	localAddressesAt = time.Now()
	return seen
}
