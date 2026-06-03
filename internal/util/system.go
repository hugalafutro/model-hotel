package util

import (
	"bufio"
	"fmt"
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

// File paths for cgroup/network stats (overridable in tests)
var (
	cgroupMemoryCurrentFile = "/sys/fs/cgroup/memory.current"
	cgroupMemoryMaxFile     = "/sys/fs/cgroup/memory.max"
	cgroupCPUStatFile       = "/sys/fs/cgroup/cpu.stat"
	cgroupIOStatFile        = "/sys/fs/cgroup/io.stat"
	cgroupProcsFile         = "/sys/fs/cgroup/cgroup.procs"
	procNetDevFile          = "/proc/net/dev"
)

// ReadCgroupMemory reads cgroup v2 memory files and returns (current, limit, inContainer).
// Returns (0, 0, false) if not in a container or files are unreadable.
func ReadCgroupMemory() (current, limit int64, inContainer bool) {
	currentBytes, err := os.ReadFile(cgroupMemoryCurrentFile)
	if err == nil {
		val := strings.TrimSpace(string(currentBytes))
		if v, e := ParseInt(val); e == nil {
			current = v
			inContainer = true
		}
	}

	limitBytes, err := os.ReadFile(cgroupMemoryMaxFile)
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
	f, err := os.Open(cgroupCPUStatFile)
	if err != nil {
		return -1
	}
	defer func() { _ = f.Close() }()

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

// NetPrevRxBytes tracks previous network receive bytes for rate calculation.
var NetPrevRxBytes int64

// NetPrevTxBytes tracks previous network transmit bytes for rate calculation.
var NetPrevTxBytes int64

// NetPrevTime tracks the previous network stats read time.
var NetPrevTime time.Time

// NetPrevMu protects network stats delta variables.
var NetPrevMu sync.Mutex

// ReadNetworkStats calculates network I/O rates from /proc/net/dev.
func ReadNetworkStats() (rxBytesPerSec, txBytesPerSec float64) {
	f, err := os.Open(procNetDevFile)
	if err != nil {
		return 0, 0
	}
	defer func() { _ = f.Close() }()

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

// DiskPrevReadBytes tracks previous disk read bytes for rate calculation.
var DiskPrevReadBytes int64

// DiskPrevWriteBytes tracks previous disk write bytes for rate calculation.
var DiskPrevWriteBytes int64

// DiskPrevTime tracks the previous disk stats read time.
var DiskPrevTime time.Time

// DiskPrevMu protects disk stats delta variables.
var DiskPrevMu sync.Mutex

// ReadCgroupDiskIO calculates disk I/O rates from cgroup io.stat.
func ReadCgroupDiskIO() (readBytesPerSec, writeBytesPerSec float64) {
	f, err := os.Open(cgroupIOStatFile)
	if err != nil {
		return -1, -1
	}
	defer func() { _ = f.Close() }()

	var totalRead, totalWrite int64
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		for _, field := range strings.Fields(line) {
			if strings.HasPrefix(field, "rbytes=") {
				if v, e := ParseInt(strings.TrimPrefix(field, "rbytes=")); e == nil {
					totalRead += v
				}
			} else if strings.HasPrefix(field, "wbytes=") {
				if v, e := ParseInt(strings.TrimPrefix(field, "wbytes=")); e == nil {
					totalWrite += v
				}
			}
		}
	}

	DiskPrevMu.Lock()
	defer DiskPrevMu.Unlock()

	if DiskPrevTime.IsZero() {
		DiskPrevTime = time.Now()
		DiskPrevReadBytes = totalRead
		DiskPrevWriteBytes = totalWrite
		return 0, 0
	}

	now := time.Now()
	deltaSec := now.Sub(DiskPrevTime).Seconds()
	deltaRead := totalRead - DiskPrevReadBytes
	deltaWrite := totalWrite - DiskPrevWriteBytes

	DiskPrevTime = now
	DiskPrevReadBytes = totalRead
	DiskPrevWriteBytes = totalWrite

	if deltaSec <= 0 {
		return 0, 0
	}

	if deltaRead > 0 {
		readBytesPerSec = float64(deltaRead) / deltaSec
	}
	if deltaWrite > 0 {
		writeBytesPerSec = float64(deltaWrite) / deltaSec
	}

	return readBytesPerSec, writeBytesPerSec
}

// ReadCgroupProcs counts processes in the current cgroup.
func ReadCgroupProcs() int {
	f, err := os.Open(cgroupProcsFile)
	if err != nil {
		return 0
	}
	defer func() { _ = f.Close() }()

	count := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) != "" {
			count++
		}
	}
	return count
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

// FormatBytes formats byte count in human-readable form with 1 decimal place
// (e.g., "1.1 GB", "512.0 KB").
func FormatBytes(b int64) string {
	const (
		KB int64 = 1024
		MB int64 = KB * 1024
		GB int64 = MB * 1024
		TB int64 = GB * 1024
	)
	switch {
	case b >= TB:
		return fmt.Sprintf("%.1f TB", float64(b)/float64(TB))
	case b >= GB:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(GB))
	case b >= MB:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(MB))
	case b >= KB:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(KB))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
