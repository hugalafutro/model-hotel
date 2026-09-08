package util

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Per-metric rate state. Each holds the previous cumulative reading for its
// source so a rate can be computed from two samples.
var (
	cpuRates  rateSampler
	netRates  rateSampler
	diskRates rateSampler
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
		// A readable memory.current is what says we are in a cgroup; whether
		// its contents parse is a separate question, and an unreadable value
		// leaves the metric at 0 rather than reporting a number nobody wrote.
		inContainer = true
		if v, e := strconv.ParseInt(strings.TrimSpace(string(currentBytes)), 10, 64); e == nil {
			current = v
		}
	}

	limitBytes, err := os.ReadFile(cgroupMemoryMaxFile)
	if err == nil {
		val := strings.TrimSpace(string(limitBytes))
		if val == "max" {
			limit = 0
		} else if v, e := strconv.ParseInt(val, 10, 64); e == nil {
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
		if after, ok := strings.CutPrefix(line, "usage_usec "); ok {
			val := after
			if v, e := strconv.ParseInt(strings.TrimSpace(val), 10, 64); e == nil {
				usageUsec = v
			}
			break
		}
	}

	if usageUsec == 0 {
		return -1
	}

	rates, ok := cpuRates.Rates(time.Now(), usageUsec)
	if !ok {
		return 0
	}
	// CPU percent = (cpu time used / wall time) * 100. usage_usec is cumulative
	// CPU microseconds across all cores, so on a multi-core system this can
	// exceed 100%.
	return min(rates[0]/1_000_000*100, 999)
}

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
		if rx, e := strconv.ParseInt(fields[0], 10, 64); e == nil {
			totalRx += rx
		}
		if tx, e := strconv.ParseInt(fields[8], 10, 64); e == nil {
			totalTx += tx
		}
	}

	rates, ok := netRates.Rates(time.Now(), totalRx, totalTx)
	if !ok {
		return 0, 0
	}
	return rates[0], rates[1]
}

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
		for field := range strings.FieldsSeq(line) {
			if after, ok := strings.CutPrefix(field, "rbytes="); ok {
				if v, e := strconv.ParseInt(after, 10, 64); e == nil {
					totalRead += v
				}
			} else if after, ok := strings.CutPrefix(field, "wbytes="); ok {
				if v, e := strconv.ParseInt(after, 10, 64); e == nil {
					totalWrite += v
				}
			}
		}
	}

	rates, ok := diskRates.Rates(time.Now(), totalRead, totalWrite)
	if !ok {
		return 0, 0
	}
	return rates[0], rates[1]
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
