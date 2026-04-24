package util

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CPU tracking state for computing container CPU percentage from cgroup v2.
var (
	CPUPrevUsage int64
	CPUPrevTime  time.Time
	CPUPrevMu    sync.Mutex
)

// ReadCgroupMemory reads cgroup v2 memory files and returns (current, limit, inContainer).
// Returns (0, 0, false) if not in a container or files are unreadable.
func ReadCgroupMemory() (current, limit int64, inContainer bool) {
	currentBytes, err := os.ReadFile("/sys/fs/cgroup/memory.current")
	if err == nil {
		val := strings.TrimSpace(string(currentBytes))
		if v, e := ParseInt(val); e == nil {
			current = v
			inContainer = true
		}
	}

	limitBytes, err := os.ReadFile("/sys/fs/cgroup/memory.max")
	if err == nil {
		val := strings.TrimSpace(string(limitBytes))
		if val == "max" {
			limit = 0
		} else if v, e := ParseInt(val); e == nil {
			limit = v
		}
	}

	return current, limit, inContainer
}

// ReadCgroupCPU returns container CPU usage percentage from cgroup v2 cpu.stat.
// It reads the cumulative usage_usec value and computes a delta-based percentage.
// Returns -1 if cgroup CPU stats are not available (not in a container).
// First call always returns 0 since it establishes the baseline.
func ReadCgroupCPU() float64 {
	f, err := os.Open("/sys/fs/cgroup/cpu.stat")
	if err != nil {
		return -1
	}
	defer f.Close()

	var usageUsec int64
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "usage_usec ") {
			val := strings.TrimPrefix(line, "usage_usec ")
			if v, e := strconv.ParseInt(strings.TrimSpace(val), 10, 64); e == nil {
				usageUsec = v
			}
			break
		}
	}

	if usageUsec == 0 {
		return -1
	}

	CPUPrevMu.Lock()
	defer CPUPrevMu.Unlock()

	if CPUPrevTime.IsZero() {
		CPUPrevTime = time.Now()
		CPUPrevUsage = usageUsec
		return 0
	}

	now := time.Now()
	deltaTime := now.Sub(CPUPrevTime).Seconds()
	deltaUsage := usageUsec - CPUPrevUsage

	CPUPrevTime = now
	CPUPrevUsage = usageUsec

	if deltaTime <= 0 || deltaUsage < 0 {
		return 0
	}

	// CPU percent = (cpu time used / wall time) * 100
	// usage_usec is cumulative CPU microseconds across all cores.
	// On a multi-core system this can exceed 100%.
	percent := (float64(deltaUsage) / (deltaTime * 1_000_000)) * 100
	if percent < 0 {
		percent = 0
	}
	if percent > 999 {
		percent = 999
	}
	return percent
}

var (
	NetPrevRxBytes int64
	NetPrevTxBytes int64
	NetPrevTime    time.Time
	NetPrevMu      sync.Mutex
)

func ReadNetworkStats() (rxBytesPerSec, txBytesPerSec float64) {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return 0, 0
	}
	defer f.Close()

	var totalRx, totalTx int64
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.Contains(line, ":") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		iface := strings.TrimSpace(parts[0])
		if iface == "lo" {
			continue
		}
		fields := strings.Fields(strings.TrimSpace(parts[1]))
		if len(fields) < 10 {
			continue
		}
		if rx, e := ParseInt(fields[0]); e == nil {
			totalRx += rx
		}
		if tx, e := ParseInt(fields[8]); e == nil {
			totalTx += tx
		}
	}

	NetPrevMu.Lock()
	defer NetPrevMu.Unlock()

	if NetPrevTime.IsZero() {
		NetPrevTime = time.Now()
		NetPrevRxBytes = totalRx
		NetPrevTxBytes = totalTx
		return 0, 0
	}

	now := time.Now()
	deltaSec := now.Sub(NetPrevTime).Seconds()
	deltaRx := totalRx - NetPrevRxBytes
	deltaTx := totalTx - NetPrevTxBytes

	NetPrevTime = now
	NetPrevRxBytes = totalRx
	NetPrevTxBytes = totalTx

	if deltaSec <= 0 {
		return 0, 0
	}

	if deltaRx > 0 {
		rxBytesPerSec = float64(deltaRx) / deltaSec
	}
	if deltaTx > 0 {
		txBytesPerSec = float64(deltaTx) / deltaSec
	}

	return rxBytesPerSec, txBytesPerSec
}

// ParseInt parses a string of digits into an int64 without using strconv
// (useful for cgroup files where strconv may be overkill).
func ParseInt(s string) (int64, error) {
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int64(c-'0')
	}
	return n, nil
}
