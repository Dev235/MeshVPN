package ipam

import (
	"testing"
)

func TestIPAMAllocationAndRelease(t *testing.T) {
	mgr, err := NewIPAM("10.100.0.0/16")
	if err != nil {
		t.Fatalf("NewIPAM failed: %v", err)
	}

	// First node allocation -> 10.100.0.2
	ip1, err := mgr.Allocate("node-1")
	if err != nil {
		t.Fatalf("Allocate node-1 failed: %v", err)
	}
	if ip1 != "10.100.0.2" {
		t.Fatalf("Expected 10.100.0.2, got %s", ip1)
	}

	// Retention check -> node-1 receives same IP
	ip1Retain, err := mgr.Allocate("node-1")
	if err != nil || ip1Retain != ip1 {
		t.Fatalf("IP retention failed: expected %s, got %s", ip1, ip1Retain)
	}

	// Second node allocation -> 10.100.0.3
	ip2, err := mgr.Allocate("node-2")
	if err != nil || ip2 != "10.100.0.3" {
		t.Fatalf("Expected 10.100.0.3, got %s", ip2)
	}

	// Release node-1 IP and verify reallocation
	mgr.Release("node-1")
	ip3, err := mgr.Allocate("node-3")
	if err != nil || ip3 != "10.100.0.2" {
		t.Fatalf("Expected re-allocated IP 10.100.0.2 for node-3, got %s", ip3)
	}
}

func TestIPAMStaticAssignmentConflict(t *testing.T) {
	mgr, _ := NewIPAM("10.100.0.0/16")

	_ = mgr.AssignStatic("node-1", "10.100.0.10")

	err := mgr.AssignStatic("node-2", "10.100.0.10")
	if err == nil {
		t.Fatal("Expected conflict error assigning same static IP to node-2!")
	}
}
