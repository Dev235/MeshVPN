package identity

import (
	"crypto/ed25519"
	"testing"
)

func TestIdentityGenerationAndSigning(t *testing.T) {
	id, err := GenerateIdentity()
	if err != nil {
		t.Fatalf("GenerateIdentity failed: %v", err)
	}

	if id.NodeID == "" {
		t.Fatal("Expected non-empty NodeID")
	}

	if id.WireGuardPrivateKey == "" || id.WireGuardPublicKey == "" {
		t.Fatal("Expected non-empty WireGuard keypair")
	}

	msg := []byte("meshvpn-test-challenge")
	sig := id.SignPayload(msg)

	if !VerifySignature(id.Ed25519PublicKey, msg, sig) {
		t.Fatal("Signature verification failed for valid key and payload")
	}

	// Tampered message test
	if VerifySignature(id.Ed25519PublicKey, []byte("tampered-payload"), sig) {
		t.Fatal("Signature verification succeeded for tampered payload!")
	}
}

func TestDeriveNodeID(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	nodeID1 := DeriveNodeID(pub)
	nodeID2 := DeriveNodeID(pub)

	if nodeID1 != nodeID2 {
		t.Fatalf("Node ID derivation is not deterministic: %s != %s", nodeID1, nodeID2)
	}

	if len(nodeID1) != 32 {
		t.Fatalf("Expected 32 hex chars for Node ID, got len %d (%s)", len(nodeID1), nodeID1)
	}
}
