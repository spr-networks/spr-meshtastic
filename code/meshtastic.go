package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// cliTimeout bounds every meshtastic CLI invocation. TCP connects to a node on
// the LAN are quick; the CLI itself needs several seconds to sync config.
var cliTimeout = 90 * time.Second

// runCommand is swapped out by the unit tests.
var runCommand = func(ctx context.Context, name string, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// runMeshtastic executes the meshtastic CLI with the config's connection flags
// plus args. User input only ever lands in argv elements (never a shell).
func runMeshtastic(cfg Config, args ...string) (string, error) {
	if err := validateConfig(cfg); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), cliTimeout)
	defer cancel()
	argv := append(cfg.connectionArgs(), args...)
	out, err := runCommand(ctx, "meshtastic", argv)
	if err != nil {
		return out, fmt.Errorf("meshtastic %s failed: %w: %s", args[0], err, lastLines(out, 3))
	}
	return out, nil
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " | ")
}

// ---- `meshtastic --info` parsing ----

// NodeUser mirrors the "user" object in the CLI's node JSON.
type NodeUser struct {
	ID        string `json:"id"`
	LongName  string `json:"longName"`
	ShortName string `json:"shortName"`
	HwModel   string `json:"hwModel"`
}

// DeviceMetrics mirrors "deviceMetrics" in the CLI's node JSON.
type DeviceMetrics struct {
	BatteryLevel       *int     `json:"batteryLevel,omitempty"`
	Voltage            *float64 `json:"voltage,omitempty"`
	ChannelUtilization *float64 `json:"channelUtilization,omitempty"`
	AirUtilTx          *float64 `json:"airUtilTx,omitempty"`
}

// MeshNode is one entry of the "Nodes in mesh:" JSON dict.
type MeshNode struct {
	Num           uint32         `json:"num"`
	User          NodeUser       `json:"user"`
	SNR           *float64       `json:"snr,omitempty"`
	LastHeard     *int64         `json:"lastHeard,omitempty"`
	HopsAway      *int           `json:"hopsAway,omitempty"`
	DeviceMetrics *DeviceMetrics `json:"deviceMetrics,omitempty"`
}

// MeshInfo is everything we extract from one `meshtastic --info` run.
type MeshInfo struct {
	Owner       string
	OwnerShort  string
	MyNodeNum   uint32
	Firmware    string
	Nodes       map[string]MeshNode // keyed by node id ("!hex")
	PrimaryURL  string
	CompleteURL string
}

var (
	ownerRe    = regexp.MustCompile(`(?m)^Owner:\s+(.*?)(?:\s+\((.*)\))?\s*$`)
	primaryRe  = regexp.MustCompile(`(?m)^Primary channel URL:\s+(\S+)`)
	completeRe = regexp.MustCompile(`(?m)^Complete URL \(includes all channels\):\s+(\S+)`)
)

// extractLabeledJSON finds `label` in out and returns the JSON object that
// starts at the first '{' after it, using brace matching that is aware of
// JSON string literals. Returns "" if not found.
func extractLabeledJSON(out, label string) string {
	idx := strings.Index(out, label)
	if idx < 0 {
		return ""
	}
	rest := out[idx+len(label):]
	start := strings.IndexByte(rest, '{')
	if start < 0 {
		return ""
	}
	depth := 0
	inStr := false
	escaped := false
	for i := start; i < len(rest); i++ {
		c := rest[i]
		if inStr {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return rest[start : i+1]
			}
		}
	}
	return ""
}

// parseInfo parses the output of `meshtastic --info`.
func parseInfo(out string) (*MeshInfo, error) {
	info := &MeshInfo{Nodes: map[string]MeshNode{}}

	if m := ownerRe.FindStringSubmatch(out); m != nil {
		info.Owner = m[1]
		info.OwnerShort = m[2]
	}

	if raw := extractLabeledJSON(out, "My info:"); raw != "" {
		var myInfo struct {
			MyNodeNum uint32 `json:"myNodeNum"`
		}
		if err := json.Unmarshal([]byte(raw), &myInfo); err == nil {
			info.MyNodeNum = myInfo.MyNodeNum
		}
	}

	if raw := extractLabeledJSON(out, "Metadata:"); raw != "" {
		var meta struct {
			FirmwareVersion string `json:"firmwareVersion"`
		}
		if err := json.Unmarshal([]byte(raw), &meta); err == nil {
			info.Firmware = meta.FirmwareVersion
		}
	}

	raw := extractLabeledJSON(out, "Nodes in mesh:")
	if raw == "" {
		return nil, fmt.Errorf("no 'Nodes in mesh' section in CLI output: %s", lastLines(out, 3))
	}
	if err := json.Unmarshal([]byte(raw), &info.Nodes); err != nil {
		return nil, fmt.Errorf("failed to parse nodes JSON: %w", err)
	}

	if m := primaryRe.FindStringSubmatch(out); m != nil {
		info.PrimaryURL = m[1]
	}
	if m := completeRe.FindStringSubmatch(out); m != nil {
		info.CompleteURL = m[1]
	}

	return info, nil
}

// self returns the node entry for the connected radio itself, if present.
func (mi *MeshInfo) self() *MeshNode {
	for _, n := range mi.Nodes {
		if n.Num == mi.MyNodeNum {
			node := n
			return &node
		}
	}
	return nil
}

// sortedNodes returns the mesh nodes ordered by lastHeard, most recent first.
func (mi *MeshInfo) sortedNodes() []MeshNode {
	nodes := make([]MeshNode, 0, len(mi.Nodes))
	for _, n := range mi.Nodes {
		nodes = append(nodes, n)
	}
	sort.Slice(nodes, func(i, j int) bool {
		var li, lj int64
		if nodes[i].LastHeard != nil {
			li = *nodes[i].LastHeard
		}
		if nodes[j].LastHeard != nil {
			lj = *nodes[j].LastHeard
		}
		if li != lj {
			return li > lj
		}
		return nodes[i].User.ID < nodes[j].User.ID
	})
	return nodes
}

// ---- cached info (a --info run takes several seconds; don't hammer the node) ----

var infoCacheTTL = 15 * time.Second

type infoCache struct {
	mu        sync.Mutex
	info      *MeshInfo
	err       error
	fetchedAt time.Time
	forConfig Config
}

var gInfoCache infoCache

// getInfo returns cached mesh info, refreshing it via the CLI when stale,
// forced, or the connection settings changed.
func getInfo(force bool) (*MeshInfo, error) {
	cfg := getConfig()

	gInfoCache.mu.Lock()
	defer gInfoCache.mu.Unlock()

	fresh := time.Since(gInfoCache.fetchedAt) < infoCacheTTL
	sameCfg := gInfoCache.forConfig == cfg
	if !force && fresh && sameCfg && (gInfoCache.info != nil || gInfoCache.err != nil) {
		return gInfoCache.info, gInfoCache.err
	}

	out, err := runMeshtastic(cfg, "--info")
	var info *MeshInfo
	if err == nil {
		info, err = parseInfo(out)
	}
	gInfoCache.info = info
	gInfoCache.err = err
	gInfoCache.fetchedAt = time.Now()
	gInfoCache.forConfig = cfg
	return info, err
}

// invalidateInfoCache drops the cache (e.g. after a config change).
func invalidateInfoCache() {
	gInfoCache.mu.Lock()
	defer gInfoCache.mu.Unlock()
	gInfoCache.info = nil
	gInfoCache.err = nil
	gInfoCache.fetchedAt = time.Time{}
}
