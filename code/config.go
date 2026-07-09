package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sync"
)

// TEST_PREFIX lets the tests point all file paths at a temp dir.
var TEST_PREFIX = os.Getenv("TEST_PREFIX")

var (
	ConfigFile       = TEST_PREFIX + "/configs/spr-meshtastic/config.json"
	MessagesFile     = TEST_PREFIX + "/state/plugins/spr-meshtastic/messages.json"
	UnixPluginSocket = TEST_PREFIX + "/state/plugins/spr-meshtastic/socket"
)

const (
	ModeTCP    = "tcp"
	ModeSerial = "serial"
)

// Config is the plugin configuration stored at /configs/spr-meshtastic/config.json.
// It holds no secrets — just how to reach the Meshtastic node.
type Config struct {
	// ConnectionMode is "tcp" (network node, port 4403) or "serial" (USB
	// passthrough, requires the commented-out devices/group_add blocks in
	// docker-compose.yml).
	ConnectionMode string
	// Host is the LAN IP of the Meshtastic node for tcp mode. RFC1918 only.
	Host string
	// SerialDevice is the tty for serial mode, e.g. /dev/ttyUSB0.
	SerialDevice string
}

var (
	configMtx sync.RWMutex
	gConfig   = defaultConfig()
)

func defaultConfig() Config {
	return Config{ConnectionMode: ModeTCP}
}

var serialDeviceRe = regexp.MustCompile(`^/dev/tty[A-Za-z0-9]+$`)

// isRFC1918 reports whether host is a private (RFC1918) IPv4 address.
// The plugin only ever talks to a node on the LAN; anything else is rejected.
func isRFC1918(host string) bool {
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}
	switch {
	case ip4[0] == 10:
		return true
	case ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31:
		return true
	case ip4[0] == 192 && ip4[1] == 168:
		return true
	}
	return false
}

func isValidSerialDevice(dev string) bool {
	return serialDeviceRe.MatchString(dev)
}

// validateConfig enforces the strict allow-list rules from the security model:
// tcp mode needs an RFC1918 IPv4 literal, serial mode needs /dev/tty[A-Za-z0-9]+.
// The validated values are the only user input that ever reaches the meshtastic
// CLI's connection flags, always as separate argv elements.
func validateConfig(c Config) error {
	switch c.ConnectionMode {
	case ModeTCP:
		if c.Host == "" {
			return errors.New("tcp mode requires Host (LAN IP of the Meshtastic node)")
		}
		if !isRFC1918(c.Host) {
			return fmt.Errorf("Host must be a private (RFC1918) IPv4 address, got %q", c.Host)
		}
	case ModeSerial:
		if !isValidSerialDevice(c.SerialDevice) {
			return fmt.Errorf("SerialDevice must match ^/dev/tty[A-Za-z0-9]+$, got %q", c.SerialDevice)
		}
	default:
		return fmt.Errorf("ConnectionMode must be %q or %q", ModeTCP, ModeSerial)
	}
	return nil
}

// configured reports whether the config points at a node at all.
func (c Config) configured() bool {
	return validateConfig(c) == nil
}

// connectionArgs returns the meshtastic CLI connection flags for the config.
// Callers must hold no assumption beyond validateConfig having passed.
func (c Config) connectionArgs() []string {
	if c.ConnectionMode == ModeSerial {
		return []string{"--port", c.SerialDevice}
	}
	return []string{"--host", c.Host}
}

func loadConfig() {
	configMtx.Lock()
	defer configMtx.Unlock()
	data, err := os.ReadFile(ConfigFile)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Println("[-] failed to read config:", err)
		}
		return
	}
	cfg := defaultConfig()
	if err := json.Unmarshal(data, &cfg); err != nil {
		fmt.Println("[-] failed to parse config:", err)
		return
	}
	gConfig = cfg
}

func getConfig() Config {
	configMtx.RLock()
	defer configMtx.RUnlock()
	return gConfig
}

func setConfig(c Config) error {
	if err := validateConfig(c); err != nil {
		return err
	}
	configMtx.Lock()
	defer configMtx.Unlock()
	gConfig = c
	return writeJSONAtomic(ConfigFile, c)
}

// writeJSONAtomic writes v as indented JSON via tmp+rename, mode 0600.
func writeJSONAtomic(path string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
