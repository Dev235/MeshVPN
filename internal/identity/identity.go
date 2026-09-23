package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/curve25519"
)

// Identity represents the cryptographic node identity.
type Identity struct {
	NodeID             string             `json:"node_id"`
	Ed25519PrivateKey  ed25519.PrivateKey `json:"ed25519_private_key"`
	Ed25519PublicKey   ed25519.PublicKey  `json:"ed25519_public_key"`
	WireGuardPrivateKey string             `json:"wireguard_private_key"`
	WireGuardPublicKey  string             `json:"wireguard_public_key"`
}

// GenerateIdentity generates a brand new Ed25519 and Curve25519 keypair.
func GenerateIdentity() (*Identity, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate ed25519 identity key: %w", err)
	}

	nodeID := DeriveNodeID(pub)

	wgPriv, wgPub, err := GenerateWireGuardKeypair()
	if err != nil {
		return nil, fmt.Errorf("failed to generate wireguard keypair: %w", err)
	}

	return &Identity{
		NodeID:              nodeID,
		Ed25519PrivateKey:   priv,
		Ed25519PublicKey:    pub,
		WireGuardPrivateKey: wgPriv,
		WireGuardPublicKey:  wgPub,
	}, nil
}

// DeriveNodeID derives a deterministic Node ID from an Ed25519 public key.
func DeriveNodeID(pub ed25519.PublicKey) string {
	hash := sha256.Sum256(pub)
	return hex.EncodeToString(hash[:16]) // 32 hex characters
}

// GenerateWireGuardKeypair generates a standard Curve25519 keypair formatted in base64.
func GenerateWireGuardKeypair() (privateKeyBase64 string, publicKeyBase64 string, err error) {
	var privKey [32]byte
	if _, err := rand.Read(privKey[:]); err != nil {
		return "", "", fmt.Errorf("failed to read random bytes for wireguard key: %w", err)
	}

	// Clamp the key per Curve25519 spec
	privKey[0] &= 248
	privKey[31] &= 127
	privKey[31] |= 64

	var pubKey [32]byte
	curve25519.ScalarBaseMult(&pubKey, &privKey)

	privateKeyBase64 = base64.StdEncoding.EncodeToString(privKey[:])
	publicKeyBase64 = base64.StdEncoding.EncodeToString(pubKey[:])
	return privateKeyBase64, publicKeyBase64, nil
}

// SignPayload signs arbitrary bytes using the node's Ed25519 private key.
func (id *Identity) SignPayload(message []byte) string {
	sig := ed25519.Sign(id.Ed25519PrivateKey, message)
	return hex.EncodeToString(sig)
}

// VerifySignature verifies an Ed25519 signature against an Ed25519 public key.
func VerifySignature(pubKey ed25519.PublicKey, message []byte, sigHex string) bool {
	sig, err := hex.DecodeString(sigHex)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(pubKey, message, sig)
}

// SaveIdentity persists identity keys securely to a file path.
func SaveIdentity(id *Identity, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("failed to create directory for identity: %w", err)
	}

	data := fmt.Sprintf("NODE_ID=%s\nED25519_PRIV=%s\nED25519_PUB=%s\nWG_PRIV=%s\nWG_PUB=%s\n",
		id.NodeID,
		hex.EncodeToString(id.Ed25519PrivateKey),
		hex.EncodeToString(id.Ed25519PublicKey),
		id.WireGuardPrivateKey,
		id.WireGuardPublicKey,
	)

	return os.WriteFile(path, []byte(data), 0600)
}

// LoadIdentity loads identity keys from a specified file path.
func LoadIdentity(path string) (*Identity, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	id := &Identity{}
	var edPrivHex, edPubHex string

	lines := splitLines(string(data))
	for _, line := range lines {
		var k, v string
		if _, err := fmt.Sscanf(line, "%s=%s", &k, &v); err == nil {
			switch k {
			case "NODE_ID":
				id.NodeID = v
			case "ED25519_PRIV":
				edPrivHex = v
			case "ED25519_PUB":
				edPubHex = v
			case "WG_PRIV":
				id.WireGuardPrivateKey = v
			case "WG_PUB":
				id.WireGuardPublicKey = v
			}
		}
	}

	if edPrivHex == "" || edPubHex == "" {
		return nil, errors.New("invalid identity file format")
	}

	priv, err := hex.DecodeString(edPrivHex)
	if err != nil || len(priv) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid private key in file: %w", err)
	}
	pub, err := hex.DecodeString(edPubHex)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid public key in file: %w", err)
	}

	id.Ed25519PrivateKey = ed25519.PrivateKey(priv)
	id.Ed25519PublicKey = ed25519.PublicKey(pub)
	return id, nil
}

func splitLines(s string) []string {
	var lines []string
	var current string
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' || s[i] == '\r' {
			if current != "" {
				lines = append(lines, current)
				current = ""
			}
		} else {
			current += string(s[i])
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}
