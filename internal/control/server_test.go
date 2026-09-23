package control

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/meshvpn/meshvpn/pkg/protocol"
)

func TestPasswordHashingAndVerification(t *testing.T) {
	password := "SecretMinecraft123"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}

	netRec := &NetworkRecord{
		Name:         "test-net",
		PasswordHash: hash,
	}

	if !netRec.CheckPassword("SecretMinecraft123") {
		t.Fatal("Expected CheckPassword to succeed for correct password")
	}

	if netRec.CheckPassword("WrongPassword") {
		t.Fatal("Expected CheckPassword to fail for incorrect password")
	}
}

func TestNetworkCreateAndJoinWithPassword(t *testing.T) {
	tempDB := "test_control_db.json"
	defer os.Remove(tempDB)

	srv, err := NewServer(":0", ":0", tempDB)
	if err != nil {
		t.Fatalf("Failed to create control server: %v", err)
	}

	// Register a dummy node first
	_ = srv.store.RegisterNode(&NodeRecord{
		NodeID:           "node-test-1",
		Ed25519PublicKey: "abcdef",
		Hostname:         "client-pc",
		IsApproved:       true,
		CreatedAt:        time.Now(),
	})

	// 1. Create password-protected network
	createReq := protocol.CreateNetworkRequest{
		NetworkName: "test-mesh",
		Password:    "meshpass123",
	}
	body, _ := json.Marshal(createReq)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/networks", bytes.NewBuffer(body))
	w := httptest.NewRecorder()
	srv.handleNetworks(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK creating network, got %d: %s", w.Code, w.Body.String())
	}

	// 2. Try joining with WRONG password
	joinWrong := protocol.JoinNetworkRequest{
		NetworkName:        "test-mesh",
		Password:           "badpass",
		NodeID:             "node-test-1",
		WireGuardPublicKey: "test-pubkey",
	}
	bodyWrong, _ := json.Marshal(joinWrong)
	reqWrong := httptest.NewRequest(http.MethodPost, "/api/v1/networks/join", bytes.NewBuffer(bodyWrong))
	wWrong := httptest.NewRecorder()
	srv.handleJoinNetwork(wWrong, reqWrong)

	if wWrong.Code != http.StatusUnauthorized {
		t.Fatalf("Expected 401 Unauthorized for wrong password, got %d", wWrong.Code)
	}

	// 3. Join with CORRECT password
	joinCorrect := protocol.JoinNetworkRequest{
		NetworkName:        "test-mesh",
		Password:           "meshpass123",
		NodeID:             "node-test-1",
		WireGuardPublicKey: "test-pubkey",
	}
	bodyCorrect, _ := json.Marshal(joinCorrect)
	reqCorrect := httptest.NewRequest(http.MethodPost, "/api/v1/networks/join", bytes.NewBuffer(bodyCorrect))
	wCorrect := httptest.NewRecorder()
	srv.handleJoinNetwork(wCorrect, reqCorrect)

	if wCorrect.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for correct password, got %d: %s", wCorrect.Code, wCorrect.Body.String())
	}

	var joinResp protocol.JoinNetworkResponse
	if err := json.NewDecoder(wCorrect.Body).Decode(&joinResp); err != nil {
		t.Fatalf("Failed to decode join response: %v", err)
	}

	if joinResp.NetworkName != "test-mesh" {
		t.Fatalf("Expected network name 'test-mesh', got '%s'", joinResp.NetworkName)
	}
	if joinResp.VirtualIP == "" {
		t.Fatal("Expected virtual IP to be assigned")
	}
}
