package ipc

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"

	"github.com/meshvpn/meshvpn/pkg/protocol"
)

// Request defines IPC command payload sent from CLI to meshvpnd.
type Request struct {
	Command     string                 `json:"command"` // 'status', 'diagnose', 'join', 'leave', etc.
	InviteToken string                 `json:"invite_token,omitempty"`
	NetworkName string                 `json:"network_name,omitempty"`
	ControlURL  string                 `json:"control_url,omitempty"`
	Args        map[string]interface{} `json:"args,omitempty"`
}

// Response defines IPC response payload returned from meshvpnd to CLI.
type Response struct {
	Success bool                   `json:"success"`
	Message string                 `json:"message,omitempty"`
	Data    map[string]interface{} `json:"data,omitempty"`
	Report  *protocol.StatusReport `json:"report,omitempty"`
}

// GetSocketPath returns standard local socket/listener address for IPC.
func GetSocketPath() string {
	if runtime.GOOS == "windows" {
		return "127.0.0.1:51821"
	}
	return "/var/run/meshvpn.sock"
}

// ListenIPC creates the daemon IPC listener connection.
func ListenIPC() (net.Listener, error) {
	path := GetSocketPath()
	if runtime.GOOS == "windows" {
		return net.Listen("tcp", path)
	}

	// Cleanup existing unix socket file if present
	_ = os.Remove(path)
	l, err := net.Listen("unix", path)
	if err == nil {
		_ = os.Chmod(path, 0660)
	}
	return l, err
}

// DialIPC connects from CLI tool to daemon.
func DialIPC() (net.Conn, error) {
	path := GetSocketPath()
	if runtime.GOOS == "windows" {
		return net.Dial("tcp", path)
	}
	return net.Dial("unix", path)
}

// SendRequest transmits IPC request payload and reads response.
func SendRequest(req *Request) (*Response, error) {
	conn, err := DialIPC()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to meshvpnd daemon: %w. Is meshvpnd running?", err)
	}
	defer conn.Close()

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		if err == io.EOF {
			return nil, fmt.Errorf("daemon closed connection unexpectedly")
		}
		return nil, fmt.Errorf("failed to decode daemon response: %w", err)
	}

	return &resp, nil
}
