package acl

import (
	"testing"

	"github.com/meshvpn/meshvpn/pkg/protocol"
)

func TestACLEvaluation(t *testing.T) {
	rules := []protocol.ACLRule{
		{
			ID:                  "rule-1",
			NetworkName:         "minecraft",
			SourceSelector:      "*",
			DestinationSelector: "10.100.0.10",
			Protocol:            "tcp",
			Port:                25565,
			Action:              "allow",
			Order:               0,
		},
		{
			ID:                  "rule-2",
			NetworkName:         "minecraft",
			SourceSelector:      "*",
			DestinationSelector: "10.100.0.10",
			Protocol:            "udp",
			Port:                25565,
			Action:              "allow",
			Order:               1,
		},
	}

	engine := NewEngine(rules)

	// Allowed Minecraft TCP traffic
	if !engine.Evaluate("10.100.0.11", "10.100.0.10", "tcp", 25565) {
		t.Fatal("Expected Minecraft TCP 25565 traffic to be ALLOWED")
	}

	// Allowed Minecraft UDP traffic
	if !engine.Evaluate("10.100.0.12", "10.100.0.10", "udp", 25565) {
		t.Fatal("Expected Minecraft UDP 25565 traffic to be ALLOWED")
	}

	// Denied SSH traffic to Minecraft server (22) -> Deny by default
	if engine.Evaluate("10.100.0.11", "10.100.0.10", "tcp", 22) {
		t.Fatal("Expected SSH port 22 traffic to be DENIED by default")
	}

	// Denied traffic to non-permitted node
	if engine.Evaluate("10.100.0.11", "10.100.0.99", "tcp", 80) {
		t.Fatal("Expected traffic to unlisted IP 10.100.0.99 to be DENIED by default")
	}
}

func TestACLConfigParsing(t *testing.T) {
	yamlText := `
network: minecraft

rules:
  - source: "*"
    destination: "10.100.0.10"
    protocol: tcp
    port: 25565
    action: allow
`

	parsed, err := ParseYAMLConfig("minecraft", yamlText)
	if err != nil {
		t.Fatalf("ParseYAMLConfig failed: %v", err)
	}

	if len(parsed) != 1 {
		t.Fatalf("Expected 1 rule parsed, got %d", len(parsed))
	}

	r := parsed[0]
	if r.SourceSelector != "*" || r.DestinationSelector != "10.100.0.10" || r.Port != 25565 || r.Action != "allow" {
		t.Fatalf("Parsed rule field mismatch: %+v", r)
	}
}
