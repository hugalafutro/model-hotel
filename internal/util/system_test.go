package util

import (
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
	} else {
		if current != 0 || limit != 0 {
			t.Errorf("Expected (0, 0, false) when not in container, got (%d, %d, %v)", current, limit, inContainer)
		}
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
