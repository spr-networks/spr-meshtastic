package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// sampleInfo mirrors the structure of real `meshtastic --info` output
// (mesh_interface.showInfo + node.showInfo in the Python CLI).
const sampleInfo = `Connected to radio

Owner: Base Station (BASE)
My info: { "myNodeNum": 862521276, "rebootCount": 13, "minAppVersion": 30200, "pioEnv": "tbeam" }
Metadata: { "firmwareVersion": "2.5.20.4c97351", "deviceStateVersion": 23, "canShutdown": true, "hasWifi": true, "role": "ROUTER", "hwModel": "TBEAM" }

Nodes in mesh: {
  "!33664ebc": {
    "num": 862521276,
    "user": {
      "id": "!33664ebc",
      "longName": "Base Station",
      "shortName": "BASE",
      "macaddr": "1AlmZU68",
      "hwModel": "TBEAM"
    },
    "position": {
      "latitudeI": 407127530,
      "longitudeI": -740059730,
      "altitude": 12,
      "time": 1719174515
    },
    "snr": 6.25,
    "lastHeard": 1719174515,
    "deviceMetrics": {
      "batteryLevel": 76,
      "voltage": 3.941,
      "channelUtilization": 3.27,
      "airUtilTx": 0.11
    }
  },
  "!7c5b91e2": {
    "num": 2086572514,
    "user": {
      "id": "!7c5b91e2",
      "longName": "Trail {Node} \"West\"",
      "shortName": "TW01",
      "hwModel": "HELTEC_V3"
    },
    "snr": -7.5,
    "lastHeard": 1719174001,
    "hopsAway": 1,
    "deviceMetrics": {
      "batteryLevel": 41,
      "voltage": 3.62
    }
  },
  "!11223344": {
    "num": 287454020,
    "user": {
      "id": "!11223344",
      "longName": "No Metrics",
      "shortName": "NM01",
      "hwModel": "RAK4631"
    }
  }
}

Preferences: { "phoneTimeout": 900, "lsSecs": 300 }
Module preferences: { "mqtt": { "enabled": false } }
Channels:
  Index 0: PRIMARY psk=default { "psk": "AQ==" }
Primary channel URL: https://meshtastic.org/e/#CgMSAQESCAgBOAFAA0gB
Complete URL (includes all channels): https://meshtastic.org/e/#CgMSAQESCAgBOAFAA0gBQQFF
`

func TestParseInfo(t *testing.T) {
	info, err := parseInfo(sampleInfo)
	if err != nil {
		t.Fatalf("parseInfo: %v", err)
	}
	if info.Owner != "Base Station" || info.OwnerShort != "BASE" {
		t.Errorf("owner = %q (%q)", info.Owner, info.OwnerShort)
	}
	if info.MyNodeNum != 862521276 {
		t.Errorf("myNodeNum = %d", info.MyNodeNum)
	}
	if info.Firmware != "2.5.20.4c97351" {
		t.Errorf("firmware = %q", info.Firmware)
	}
	if len(info.Nodes) != 3 {
		t.Fatalf("nodes = %d, want 3", len(info.Nodes))
	}

	base := info.Nodes["!33664ebc"]
	if base.User.LongName != "Base Station" || base.User.HwModel != "TBEAM" {
		t.Errorf("base node user = %+v", base.User)
	}
	if base.SNR == nil || *base.SNR != 6.25 {
		t.Errorf("base snr = %v", base.SNR)
	}
	if base.LastHeard == nil || *base.LastHeard != 1719174515 {
		t.Errorf("base lastHeard = %v", base.LastHeard)
	}
	if base.DeviceMetrics == nil || base.DeviceMetrics.BatteryLevel == nil || *base.DeviceMetrics.BatteryLevel != 76 {
		t.Errorf("base metrics = %+v", base.DeviceMetrics)
	}

	// braces + escaped quotes inside a longName must not break brace matching
	trail := info.Nodes["!7c5b91e2"]
	if trail.User.LongName != `Trail {Node} "West"` {
		t.Errorf("trail longName = %q", trail.User.LongName)
	}
	if trail.HopsAway == nil || *trail.HopsAway != 1 {
		t.Errorf("trail hopsAway = %v", trail.HopsAway)
	}

	// missing optional fields stay nil
	nm := info.Nodes["!11223344"]
	if nm.SNR != nil || nm.LastHeard != nil || nm.DeviceMetrics != nil {
		t.Errorf("expected nil optionals, got %+v", nm)
	}

	if info.PrimaryURL != "https://meshtastic.org/e/#CgMSAQESCAgBOAFAA0gB" {
		t.Errorf("primary url = %q", info.PrimaryURL)
	}
	if info.CompleteURL != "https://meshtastic.org/e/#CgMSAQESCAgBOAFAA0gBQQFF" {
		t.Errorf("complete url = %q", info.CompleteURL)
	}

	self := info.self()
	if self == nil || self.User.ID != "!33664ebc" {
		t.Errorf("self = %+v", self)
	}

	sorted := info.sortedNodes()
	ids := []string{sorted[0].User.ID, sorted[1].User.ID, sorted[2].User.ID}
	want := []string{"!33664ebc", "!7c5b91e2", "!11223344"}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("sorted = %v, want %v", ids, want)
	}
}

func TestParseInfoNoNodes(t *testing.T) {
	if _, err := parseInfo("Connected to radio\nOwner: X (Y)\n"); err == nil {
		t.Error("expected error when Nodes in mesh section is missing")
	}
}

func TestExtractLabeledJSON(t *testing.T) {
	out := `Label: { "a": "close } brace \" in string", "b": {"c": 1} } trailing`
	got := extractLabeledJSON(out, "Label:")
	want := `{ "a": "close } brace \" in string", "b": {"c": 1} }`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if extractLabeledJSON(out, "Missing:") != "" {
		t.Error("expected empty result for missing label")
	}
	if extractLabeledJSON("Label: no json here", "Label:") != "" {
		t.Error("expected empty result when no object follows")
	}
	if extractLabeledJSON("Label: { unterminated", "Label:") != "" {
		t.Error("expected empty result for unterminated object")
	}
}

func TestRunMeshtasticArgv(t *testing.T) {
	old := runCommand
	defer func() { runCommand = old }()

	var gotName string
	var gotArgs []string
	runCommand = func(ctx context.Context, name string, args []string) (string, error) {
		gotName = name
		gotArgs = args
		return "ok", nil
	}

	cfg := Config{ConnectionMode: ModeTCP, Host: "192.168.1.50"}
	if _, err := runMeshtastic(cfg, "--sendtext", "hi there; $(reboot)"); err != nil {
		t.Fatalf("runMeshtastic: %v", err)
	}
	if gotName != "meshtastic" {
		t.Errorf("command = %q", gotName)
	}
	// shell metacharacters stay inside a single argv element
	want := []string{"--host", "192.168.1.50", "--sendtext", "hi there; $(reboot)"}
	if !reflect.DeepEqual(gotArgs, want) {
		t.Errorf("argv = %v, want %v", gotArgs, want)
	}

	// unconfigured / invalid config never spawns the CLI
	runCommand = func(ctx context.Context, name string, args []string) (string, error) {
		t.Error("runCommand called for invalid config")
		return "", nil
	}
	if _, err := runMeshtastic(Config{ConnectionMode: ModeTCP, Host: "8.8.8.8"}, "--info"); err == nil {
		t.Error("expected error for invalid config")
	}
}

func TestRunMeshtasticErrorIncludesOutputTail(t *testing.T) {
	old := runCommand
	defer func() { runCommand = old }()
	runCommand = func(ctx context.Context, name string, args []string) (string, error) {
		return "line1\nline2\nConnection refused", errors.New("exit status 1")
	}
	_, err := runMeshtastic(Config{ConnectionMode: ModeTCP, Host: "10.0.0.2"}, "--info")
	if err == nil || !strings.Contains(err.Error(), "Connection refused") {
		t.Errorf("err = %v, want output tail included", err)
	}
}
