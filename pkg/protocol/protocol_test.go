package protocol

import (
	"bytes"
	"testing"
)

func TestRelayHeaderSerialization(t *testing.T) {
	var targetNodeID [16]byte
	copy(targetNodeID[:], []byte("node-12345678901"))

	header := &RelayHeader{
		Magic:         RelayMagic,
		Version:       RelayVersion,
		PacketType:    RelayTypeData,
		PayloadLength: 1420,
		TargetNodeID:  targetNodeID,
	}

	serialized := header.Serialize()
	if len(serialized) != RelayHeaderLen {
		t.Fatalf("Expected header len %d, got %d", RelayHeaderLen, len(serialized))
	}

	deserialized, err := DeserializeRelayHeader(serialized)
	if err != nil {
		t.Fatalf("DeserializeRelayHeader failed: %v", err)
	}

	if deserialized.Magic != RelayMagic || deserialized.PacketType != RelayTypeData || deserialized.PayloadLength != 1420 {
		t.Fatalf("Deserialized header field mismatch: %+v", deserialized)
	}

	if !bytes.Equal(deserialized.TargetNodeID[:], targetNodeID[:]) {
		t.Fatalf("TargetNodeID mismatch: %v != %v", deserialized.TargetNodeID, targetNodeID)
	}
}
