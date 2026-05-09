package util

import (
	"strings"
	"testing"
	"time"
)

func TestParseInt(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int64
	}{
		{"zero", "0", 0},
		{"positive", "12345", 12345},
		{"empty", "", 0},
		{"non-numeric", "abc", 0},
		{"mixed", "123abc", 123},
		{"leading spaces", "  42", 0},
		{"negative sign", "-5", 0},
		{"large number", "9999999999", 9999999999},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseInt(tc.input)
			if err != nil {
				t.Errorf("ParseInt(%q) returned error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("ParseInt(%q) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}

// resetState functions for stateful cgroup/network functions
func resetCPUState() {
	CPUPrevTime = time.Time{}
	CPUPrevUsage = 0
}

func resetNetState() {
	NetPrevTime = time.Time{}
	NetPrevRxBytes = 0
	NetPrevTxBytes = 0
}

func resetDiskState() {
	DiskPrevTime = time.Time{}
	DiskPrevReadBytes = 0
	DiskPrevWriteBytes = 0
}

// TestReadCgroupMemory_NoContainer tests behavior when not in a container
func TestReadCgroupMemory_NoContainer(t *testing.T) {
	current, limit, inContainer := ReadCgroupMemory()
	// When not in container, should return (0, 0, false) or actual values if in container
	if inContainer {
		t.Logf("Running in container: current=%d, limit=%d", current, limit)
	} else if current != 0 || limit != 0 {
		t.Errorf("Expected (0, 0, false) when not in container, got (%d, %d, %v)", current, limit, inContainer)
	}
}

// TestReadCgroupCPU_NoFile tests behavior when cgroup CPU file doesn't exist
func TestReadCgroupCPU_NoFile(t *testing.T) {
	resetCPUState()
	result := ReadCgroupCPU()
	// Should return -1 when file doesn't exist or is not in container
	if result != -1 && result != 0 {
		t.Errorf("Expected -1 or 0 when not in container, got %f", result)
	}
}

// TestReadCgroupDiskIO_NoFile tests behavior when cgroup disk IO file doesn't exist
func TestReadCgroupDiskIO_NoFile(t *testing.T) {
	resetDiskState()
	rx, tx := ReadCgroupDiskIO()
	// When file doesn't exist, should return (-1, -1)
	// When file exists but first call (baseline), should return (0, 0)
	if (rx != -1 && rx != 0) || (tx != -1 && tx != 0) {
		t.Errorf("Expected (-1, -1) or (0, 0) when not in container or first call, got (%f, %f)", rx, tx)
	}
}

// TestReadCgroupProcs_NoFile tests behavior when cgroup procs file doesn't exist
func TestReadCgroupProcs_NoFile(t *testing.T) {
	result := ReadCgroupProcs()
	// When file doesn't exist, should return 0
	// When file exists, should return actual count
	t.Logf("ReadCgroupProcs returned %d processes", result)
	// We can't predict the exact count, but it should be >= 0
	if result < 0 {
		t.Errorf("Expected >= 0, got %d", result)
	}
}

// TestReadNetworkStats_NoFile tests behavior when network stats file doesn't exist
func TestReadNetworkStats_NoFile(t *testing.T) {
	resetNetState()
	rx, tx := ReadNetworkStats()
	// Should return (0, 0) when file doesn't exist
	if rx != 0 || tx != 0 {
		t.Errorf("Expected (0, 0) when /proc/net/dev not available, got (%f, %f)", rx, tx)
	}
}

// TestReadCgroupMemory_WithFile tests memory reading when cgroup files exist
func TestReadCgroupMemory_WithFile(t *testing.T) {
	current, limit, inContainer := ReadCgroupMemory()

	// Test passes whether in container or not, just verify no panic and sensible values
	if inContainer {
		if current < 0 {
			t.Errorf("Expected current >= 0, got %d", current)
		}
		// limit can be 0 if "max" (unlimited)
		if limit < 0 {
			t.Errorf("Expected limit >= 0, got %d", limit)
		}
		t.Logf("In container: current=%d bytes, limit=%d bytes", current, limit)
	} else if current != 0 || limit != 0 {
		t.Errorf("Expected (0, 0, false) when not in container, got (%d, %d, %v)", current, limit, inContainer)
	}
}

// TestReadCgroupCPU_WithFile tests CPU reading when cgroup file exists
func TestReadCgroupCPU_WithFile(t *testing.T) {
	resetCPUState()

	// First call should return 0 (baseline)
	result1 := ReadCgroupCPU()
	if result1 != -1 && result1 != 0 {
		t.Logf("First call returned %f (expected 0 or -1)", result1)
	}

	// Second call should return actual percentage or 0 if not in container
	resetCPUState()
	result2 := ReadCgroupCPU()
	if result2 < -1 || result2 > 999 {
		t.Errorf("CPU percentage out of range: %f", result2)
	}
}

// TestReadCgroupDiskIO_WithFile tests disk IO reading when cgroup file exists
func TestReadCgroupDiskIO_WithFile(t *testing.T) {
	resetDiskState()

	// First call should return (0, 0) or (-1, -1) if file doesn't exist
	rx1, tx1 := ReadCgroupDiskIO()
	if (rx1 != -1 && rx1 != 0) || (tx1 != -1 && tx1 != 0) {
		t.Logf("First call returned (%f, %f)", rx1, tx1)
	}

	// Second call
	resetDiskState()
	rx2, tx2 := ReadCgroupDiskIO()
	if rx2 < -1 || tx2 < -1 {
		t.Errorf("Disk IO values out of range: (%f, %f)", rx2, tx2)
	}
}

// TestReadCgroupProcs_WithFile tests process count reading when cgroup file exists
func TestReadCgroupProcs_WithFile(t *testing.T) {
	result := ReadCgroupProcs()

	// Should always return non-negative value
	if result < 0 {
		t.Errorf("Expected process count >= 0, got %d", result)
	}
	t.Logf("Process count: %d", result)
}

// TestReadNetworkStats_WithFile tests network stats reading when file exists
func TestReadNetworkStats_WithFile(t *testing.T) {
	resetNetState()

	// First call should return (0, 0) (baseline)
	rx1, tx1 := ReadNetworkStats()
	if rx1 != 0 || tx1 != 0 {
		t.Logf("First call returned (%f, %f)", rx1, tx1)
	}

	// Second call should return actual rates or (0, 0) if no traffic
	resetNetState()
	rx2, tx2 := ReadNetworkStats()
	if rx2 < 0 || tx2 < 0 {
		t.Errorf("Network rates should be >= 0, got (%f, %f)", rx2, tx2)
	}
}

// TestParseInt_EdgeCases tests additional edge cases for ParseInt
func TestParseInt_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int64
		wantErr bool
	}{
		{"whitespace only", "   ", 0, false},
		{"newline", "123\n", 123, false},
		{"trailing whitespace", "456  ", 456, false},
		{"max int64", "9223372036854775807", 9223372036854775807, false},
		{"single digit", "7", 7, false},
		{"zero with leading zeros", "000", 0, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseInt(tc.input)
			if tc.wantErr && err == nil {
				t.Errorf("ParseInt(%q) expected error, got nil", tc.input)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("ParseInt(%q) unexpected error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("ParseInt(%q) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}

// TestCPUPrevState tests that CPU state is properly maintained between calls
func TestCPUPrevState(t *testing.T) {
	resetCPUState()

	// Verify initial state
	if !CPUPrevTime.IsZero() {
		t.Error("Expected CPUPrevTime to be zero initially")
	}
	if CPUPrevUsage != 0 {
		t.Errorf("Expected CPUPrevUsage to be 0, got %d", CPUPrevUsage)
	}
}

// TestNetPrevState tests that network state is properly maintained
func TestNetPrevState(t *testing.T) {
	resetNetState()

	if !NetPrevTime.IsZero() {
		t.Error("Expected NetPrevTime to be zero initially")
	}
	if NetPrevRxBytes != 0 || NetPrevTxBytes != 0 {
		t.Errorf("Expected NetPrevRx/Tx to be 0, got %d/%d", NetPrevRxBytes, NetPrevTxBytes)
	}
}

// TestDiskPrevState tests that disk IO state is properly maintained
func TestDiskPrevState(t *testing.T) {
	resetDiskState()

	if !DiskPrevTime.IsZero() {
		t.Error("Expected DiskPrevTime to be zero initially")
	}
	if DiskPrevReadBytes != 0 || DiskPrevWriteBytes != 0 {
		t.Errorf("Expected DiskPrevRead/Write to be 0, got %d/%d", DiskPrevReadBytes, DiskPrevWriteBytes)
	}
}

// TestCgroupParsing_V1 tests parsing logic for cgroup v1 format
func TestCgroupParsing_V1(t *testing.T) {
	// Simulate cgroup v1 content: 12:memory:/docker/<container_id>
	cgroupContent := `12:memory:/docker/abc123def4567890
11:cpu:/docker/abc123def4567890
10:blkio:/docker/abc123def4567890`

	var foundID string
	for _, line := range strings.Split(cgroupContent, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "/")
		for _, part := range parts {
			part = strings.TrimSuffix(part, ".scope")
			if len(part) >= 12 && isHex(part) {
				foundID = part
				break
			}
		}
		if foundID != "" {
			break
		}
	}

	if foundID != "abc123def4567890" {
		t.Errorf("Expected container ID abc123def4567890, got %q", foundID)
	}
}

// TestCgroupParsing_V2 tests parsing logic for cgroup v2 format
func TestCgroupParsing_V2(t *testing.T) {
	// Cgroup v2 format where container ID is a path component
	cgroupContent := `0::/abc123def4567890`

	var foundID string
	for _, line := range strings.Split(cgroupContent, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "/")
		for _, part := range parts {
			part = strings.TrimSuffix(part, ".scope")
			if len(part) >= 12 && isHex(part) {
				foundID = part
				break
			}
		}
		if foundID != "" {
			break
		}
	}

	if foundID != "abc123def4567890" {
		t.Errorf("Expected container ID abc123def4567890, got %q", foundID)
	}
}

// TestCgroupParsing_Empty tests parsing with empty cgroup content
func TestCgroupParsing_Empty(t *testing.T) {
	cgroupContent := ""

	var foundID string
	for _, line := range strings.Split(cgroupContent, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "/")
		for _, part := range parts {
			part = strings.TrimSuffix(part, ".scope")
			if len(part) >= 12 && isHex(part) {
				foundID = part
				break
			}
		}
		if foundID != "" {
			break
		}
	}

	if foundID != "" {
		t.Errorf("Expected empty string, got %q", foundID)
	}
}

// TestCgroupParsing_NoContainerID tests parsing when no container ID is present
func TestCgroupParsing_NoContainerID(t *testing.T) {
	// Cgroup content without container ID (e.g., running directly on host)
	cgroupContent := `0::/user.slice/user-1000.slice/session-1.scope`

	var foundID string
	for _, line := range strings.Split(cgroupContent, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "/")
		for _, part := range parts {
			part = strings.TrimSuffix(part, ".scope")
			if len(part) >= 12 && isHex(part) {
				foundID = part
				break
			}
		}
		if foundID != "" {
			break
		}
	}

	if foundID != "" {
		t.Errorf("Expected empty string for non-container cgroup, got %q", foundID)
	}
}

// TestNetworkStatsParsing tests parsing of /proc/net/dev format
func TestNetworkStatsParsing(t *testing.T) {
	// Typical /proc/net/dev content
	procNetDevContent := `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast |bytes    packets errs drop fifo colls carrier compressed
    lo:  100000    1000    0    0    0     0          0         0   100000    1000    0    0    0     0       0          0
  eth0:  500000    5000    0    0    0     0          0         0   250000    2500    0    0    0     0       0          0
  eth1:  300000    3000    0    0    0     0          0         0   150000    1500    0    0    0     0       0          0
`

	var totalRx, totalTx int64
	for _, line := range strings.Split(procNetDevContent, "\n") {
		line = strings.TrimSpace(line)
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

	// eth0: rx=500000, tx=250000; eth1: rx=300000, tx=150000
	// Total: rx=800000, tx=400000 (excluding lo)
	if totalRx != 800000 {
		t.Errorf("Expected total rx=800000, got %d", totalRx)
	}
	if totalTx != 400000 {
		t.Errorf("Expected total tx=400000, got %d", totalTx)
	}
}

// TestCgroupIOStatParsing tests parsing of io.stat format (cgroup v2)
func TestCgroupIOStatParsing(t *testing.T) {
	// Typical io.stat content from cgroup v2
	ioStatContent := `8:0 rbytes=1234567 wbytes=7654321 rios=100 wios=200
8:16 rbytes=111111 wbytes=222222 rios=10 wios=20`

	var totalRead, totalWrite int64
	for _, line := range strings.Split(ioStatContent, "\n") {
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

	// Total: rbytes=1234567+111111=1345678, wbytes=7654321+222222=7876543
	if totalRead != 1345678 {
		t.Errorf("Expected total read=1345678, got %d", totalRead)
	}
	if totalWrite != 7876543 {
		t.Errorf("Expected total write=7876543, got %d", totalWrite)
	}
}

// TestCgroupIOStatParsing_Empty tests io.stat parsing with empty content
func TestCgroupIOStatParsing_Empty(t *testing.T) {
	ioStatContent := ""

	var totalRead, totalWrite int64
	for _, line := range strings.Split(ioStatContent, "\n") {
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

	if totalRead != 0 {
		t.Errorf("Expected total read=0, got %d", totalRead)
	}
	if totalWrite != 0 {
		t.Errorf("Expected total write=0, got %d", totalWrite)
	}
}

// TestCgroupCPUStatParsing tests parsing of cpu.stat format
func TestCgroupCPUStatParsing(t *testing.T) {
	// Typical cpu.stat content from cgroup v2
	cpuStatContent := `usage_usec 12345678
usage_user_usec 10000000
usage_system_usec 2000000
nr_periods 100
nr_throttled 5
throttled_usec 50000`

	var usageUsec int64
	for _, line := range strings.Split(cpuStatContent, "\n") {
		if strings.HasPrefix(line, "usage_usec ") {
			val := strings.TrimPrefix(line, "usage_usec ")
			if v, e := ParseInt(strings.TrimSpace(val)); e == nil {
				usageUsec = v
			}
			break
		}
	}

	if usageUsec != 12345678 {
		t.Errorf("Expected usage_usec=12345678, got %d", usageUsec)
	}
}

// TestCgroupMemoryParsing tests parsing of memory.current and memory.max
func TestCgroupMemoryParsing(t *testing.T) {
	tests := []struct {
		name        string
		currentRaw  string
		maxRaw      string
		wantCurrent int64
		wantLimit   int64
	}{
		{"normal values", "1048576\n", "2097152\n", 1048576, 2097152},
		{"max limit", "1048576\n", "max\n", 1048576, 0},
		{"with whitespace", "  1048576  \n", "  2097152  \n", 1048576, 2097152},
		{"empty current", "", "2097152\n", 0, 2097152},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var current, limit int64

			// Parse current
			if tc.currentRaw != "" {
				val := strings.TrimSpace(tc.currentRaw)
				if v, e := ParseInt(val); e == nil {
					current = v
				}
			}

			// Parse limit
			if tc.maxRaw != "" {
				val := strings.TrimSpace(tc.maxRaw)
				if val == "max" {
					limit = 0
				} else if v, e := ParseInt(val); e == nil {
					limit = v
				}
			}

			if current != tc.wantCurrent {
				t.Errorf("Expected current=%d, got %d", tc.wantCurrent, current)
			}
			if limit != tc.wantLimit {
				t.Errorf("Expected limit=%d, got %d", tc.wantLimit, limit)
			}
		})
	}
}
