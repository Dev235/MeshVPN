package wireguard

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/meshvpn/meshvpn/pkg/protocol"
)

// Config represents full WireGuard configuration.
type Config struct {
	InterfaceName string
	PrivateKey    string
	VirtualIP     string // e.g. 10.100.0.3/16
	ListenPort    int    // e.g. 51820
	MTU           int    // e.g. 1420
	Peers         []PeerConfig
}

// PeerConfig represents WireGuard peer settings.
type PeerConfig struct {
	PublicKey           string
	AllowedIPs          string // e.g. 10.100.0.4/32
	Endpoint            string // e.g. 203.0.113.5:51820 or 127.0.0.1:41641
	PersistentKeepalive int    // e.g. 25
}

// Manager controls WireGuard interface operations.
type Manager struct {
	InterfaceName string
	ConfigDir     string
}

// NewManager initializes WireGuard manager.
func NewManager(interfaceName string) *Manager {
	if interfaceName == "" {
		interfaceName = "mesh0"
	}
	return &Manager{
		InterfaceName: interfaceName,
		ConfigDir:     filepath.Join(os.TempDir(), "meshvpn-wg"),
	}
}

// GenerateConfigFile renders a valid WireGuard wg0.conf string.
func (m *Manager) GenerateConfigFile(cfg Config) string {
	var sb strings.Builder

	sb.WriteString("[Interface]\n")
	sb.WriteString(fmt.Sprintf("PrivateKey = %s\n", cfg.PrivateKey))
	if cfg.VirtualIP != "" {
		sb.WriteString(fmt.Sprintf("Address = %s\n", cfg.VirtualIP))
	}
	if cfg.ListenPort > 0 {
		sb.WriteString(fmt.Sprintf("ListenPort = %d\n", cfg.ListenPort))
	}
	if cfg.MTU > 0 {
		sb.WriteString(fmt.Sprintf("MTU = %d\n", cfg.MTU))
	} else {
		sb.WriteString("MTU = 1420\n")
	}
	sb.WriteString("\n")

	for _, peer := range cfg.Peers {
		sb.WriteString("[Peer]\n")
		sb.WriteString(fmt.Sprintf("PublicKey = %s\n", peer.PublicKey))
		sb.WriteString(fmt.Sprintf("AllowedIPs = %s\n", peer.AllowedIPs))
		if peer.Endpoint != "" {
			sb.WriteString(fmt.Sprintf("Endpoint = %s\n", peer.Endpoint))
		}
		if peer.PersistentKeepalive > 0 {
			sb.WriteString(fmt.Sprintf("PersistentKeepalive = %d\n", peer.PersistentKeepalive))
		} else {
			sb.WriteString("PersistentKeepalive = 25\n")
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// WriteConfigFile writes configuration to disk.
func (m *Manager) WriteConfigFile(cfg Config) (string, error) {
	if err := os.MkdirAll(m.ConfigDir, 0700); err != nil {
		return "", fmt.Errorf("failed to create config dir: %w", err)
	}

	confPath := filepath.Join(m.ConfigDir, fmt.Sprintf("%s.conf", m.InterfaceName))
	content := m.GenerateConfigFile(cfg)

	if err := os.WriteFile(confPath, []byte(content), 0600); err != nil {
		return "", fmt.Errorf("failed to write wg config file: %w", err)
	}

	return confPath, nil
}

// SyncConfig applies WireGuard settings to the system interface.
func (m *Manager) SyncConfig(cfg Config) error {
	confPath, err := m.WriteConfigFile(cfg)
	if err != nil {
		return err
	}

	if runtime.GOOS == "windows" {
		// On Windows, use wireguard.exe or wg.exe
		cmd := exec.Command("wg", "setconf", m.InterfaceName, confPath)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("windows wg setconf failed: %s (%w)", string(output), err)
		}
		return nil
	}

	// On Linux, use `wg syncconf` or `wg-quick`
	cmd := exec.Command("wg", "syncconf", m.InterfaceName, confPath)
	if _, err := cmd.CombinedOutput(); err != nil {
		// Fallback to setting up interface via ip link / wg set if interface not up
		return m.setupLinuxInterface(cfg, confPath)
	}

	return nil
}

func (m *Manager) setupLinuxInterface(cfg Config, confPath string) error {
	// Attempt ip link add dev mesh0 type wireguard
	_ = exec.Command("ip", "link", "add", "dev", m.InterfaceName, "type", "wireguard").Run()
	_ = exec.Command("wg", "setconf", m.InterfaceName, confPath).Run()

	if cfg.VirtualIP != "" {
		_ = exec.Command("ip", "address", "add", cfg.VirtualIP, "dev", m.InterfaceName).Run()
	}

	_ = exec.Command("ip", "link", "set", "mtu", "1420", "up", "dev", m.InterfaceName).Run()
	return nil
}

// BuildPeersFromProtocol converts protocol.PeerNode slice to WireGuard PeerConfig.
func BuildPeersFromProtocol(peers []protocol.PeerNode) []PeerConfig {
	var wgPeers []PeerConfig
	for _, p := range peers {
		if p.WireGuardPublicKey == "" || p.VirtualIP == "" {
			continue
		}

		endpointStr := ""
		// Pick highest priority endpoint candidate
		for _, ep := range p.Endpoints {
			if ep.IP != "" && ep.Port > 0 {
				endpointStr = fmt.Sprintf("%s:%d", ep.IP, ep.Port)
				break
			}
		}

		wgPeers = append(wgPeers, PeerConfig{
			PublicKey:           p.WireGuardPublicKey,
			AllowedIPs:          fmt.Sprintf("%s/32", p.VirtualIP),
			Endpoint:            endpointStr,
			PersistentKeepalive: 25,
		})
	}
	return wgPeers
}
