package acl

import (
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/meshvpn/meshvpn/pkg/protocol"
)

// Engine evaluates network packet traffic against defined ACL rules.
type Engine struct {
	mu    sync.RWMutex
	rules []protocol.ACLRule
}

// NewEngine initializes an ACL evaluation engine with deny-by-default policies.
func NewEngine(initialRules []protocol.ACLRule) *Engine {
	e := &Engine{}
	e.SetRules(initialRules)
	return e
}

// SetRules updates and sorts active ACL rules by priority order.
func (e *Engine) SetRules(rules []protocol.ACLRule) {
	e.mu.Lock()
	defer e.mu.Unlock()

	sorted := make([]protocol.ACLRule, len(rules))
	copy(sorted, rules)

	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Order < sorted[j].Order
	})

	e.rules = sorted
}

// Evaluate checks whether a packet matching (srcIP, dstIP, protocol, port) is allowed.
// Default action is DENY if no explicit rule matches.
func (e *Engine) Evaluate(srcIP, dstIP, proto string, port int) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	protoLower := strings.ToLower(proto)

	for _, rule := range e.rules {
		if matchSelector(rule.SourceSelector, srcIP) &&
			matchSelector(rule.DestinationSelector, dstIP) &&
			matchProtocol(rule.Protocol, protoLower) &&
			matchPort(rule.Port, port) {
			return strings.EqualFold(rule.Action, "allow")
		}
	}

	// Deny by default
	return false
}

func matchSelector(selector, targetIP string) bool {
	selector = strings.TrimSpace(selector)
	if selector == "*" || selector == "any" {
		return true
	}

	// Direct IP match
	if selector == targetIP {
		return true
	}

	// CIDR match (e.g. 10.100.0.0/16)
	if strings.Contains(selector, "/") {
		_, ipNet, err := net.ParseCIDR(selector)
		if err == nil {
			ip := net.ParseIP(targetIP)
			if ip != nil && ipNet.Contains(ip) {
				return true
			}
		}
	}

	return false
}

func matchProtocol(ruleProto, targetProto string) bool {
	ruleProto = strings.ToLower(strings.TrimSpace(ruleProto))
	if ruleProto == "any" || ruleProto == "*" || ruleProto == "" {
		return true
	}
	return ruleProto == targetProto
}

func matchPort(rulePort, targetPort int) bool {
	if rulePort == 0 {
		return true // 0 matches any port
	}
	return rulePort == targetPort
}

// ParseYAMLConfig parses a YAML ACL string into ACLRule slices.
func ParseYAMLConfig(networkName, yamlContent string) ([]protocol.ACLRule, error) {
	// Simple robust zero-dependency YAML rule parser
	lines := strings.Split(yamlContent, "\n")
	var rules []protocol.ACLRule

	var currentRule *protocol.ACLRule
	ruleCount := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if strings.HasPrefix(trimmed, "- source:") || strings.HasPrefix(trimmed, "source:") {
			if currentRule != nil {
				rules = append(rules, *rulesPtr(currentRule, networkName, ruleCount))
				ruleCount++
			}
			currentRule = &protocol.ACLRule{Action: "deny"}
			val := getVal(trimmed, "source:")
			currentRule.SourceSelector = val
		} else if currentRule != nil {
			if strings.HasPrefix(trimmed, "destination:") {
				currentRule.DestinationSelector = getVal(trimmed, "destination:")
			} else if strings.HasPrefix(trimmed, "protocol:") {
				currentRule.Protocol = getVal(trimmed, "protocol:")
			} else if strings.HasPrefix(trimmed, "port:") {
				p, _ := strconv.Atoi(getVal(trimmed, "port:"))
				currentRule.Port = p
			} else if strings.HasPrefix(trimmed, "action:") {
				currentRule.Action = getVal(trimmed, "action:")
			}
		}
	}

	if currentRule != nil {
		rules = append(rules, *rulesPtr(currentRule, networkName, ruleCount))
	}

	return rules, nil
}

func rulesPtr(r *protocol.ACLRule, netName string, idx int) *protocol.ACLRule {
	r.NetworkName = netName
	r.Order = idx
	r.ID = fmt.Sprintf("%s-rule-%d", netName, idx+1)
	return r
}

func getVal(line, key string) string {
	parts := strings.SplitN(line, key, 2)
	if len(parts) < 2 {
		return ""
	}
	v := strings.TrimSpace(parts[1])
	v = strings.Trim(v, `"'`)
	return v
}
