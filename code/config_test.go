package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestIsRFC1918(t *testing.T) {
	valid := []string{"10.0.0.1", "10.255.255.254", "172.16.0.1", "172.31.4.2", "192.168.1.100"}
	for _, ip := range valid {
		if !isRFC1918(ip) {
			t.Errorf("expected %q to be RFC1918", ip)
		}
	}
	invalid := []string{
		"", "8.8.8.8", "1.1.1.1", "172.15.0.1", "172.32.0.1", "192.169.1.1",
		"11.0.0.1", "127.0.0.1", "169.254.1.1", "100.64.0.1",
		"fd00::1", "::1", "2001:db8::1",
		"192.168.1.1/24", "192.168.1", "meshtastic.local", "192.168.1.100; rm -rf /",
	}
	for _, ip := range invalid {
		if isRFC1918(ip) {
			t.Errorf("expected %q to be rejected", ip)
		}
	}
}

func TestIsValidSerialDevice(t *testing.T) {
	valid := []string{"/dev/ttyUSB0", "/dev/ttyACM0", "/dev/ttyAMA0", "/dev/ttyS1"}
	for _, d := range valid {
		if !isValidSerialDevice(d) {
			t.Errorf("expected %q to be valid", d)
		}
	}
	invalid := []string{
		"", "/dev/tty", "/dev/ttyUSB0/../../etc/passwd", "/dev/ttyUSB0 --foo",
		"ttyUSB0", "/dev/sda", "/dev/ttyUSB-0", "/dev/ttyUSB0\n", " /dev/ttyUSB0",
		"/dev/tty/USB0",
	}
	for _, d := range invalid {
		if isValidSerialDevice(d) {
			t.Errorf("expected %q to be rejected", d)
		}
	}
}

func TestValidateConfig(t *testing.T) {
	ok := []Config{
		{ConnectionMode: ModeTCP, Host: "192.168.1.100"},
		{ConnectionMode: ModeSerial, SerialDevice: "/dev/ttyUSB0"},
	}
	for _, c := range ok {
		if err := validateConfig(c); err != nil {
			t.Errorf("expected %+v to validate, got %v", c, err)
		}
	}
	bad := []Config{
		{},
		{ConnectionMode: "bluetooth"},
		{ConnectionMode: ModeTCP}, // missing host
		{ConnectionMode: ModeTCP, Host: "8.8.8.8"},         // not RFC1918
		{ConnectionMode: ModeTCP, Host: "example.com"},     // not an IP
		{ConnectionMode: ModeSerial},                       // missing device
		{ConnectionMode: ModeSerial, SerialDevice: "/tmp"}, // bad path
		{ConnectionMode: ModeSerial, SerialDevice: "/dev/ttyUSB0; reboot"},
	}
	for _, c := range bad {
		if err := validateConfig(c); err == nil {
			t.Errorf("expected %+v to be rejected", c)
		}
	}
}

func TestConnectionArgs(t *testing.T) {
	tcp := Config{ConnectionMode: ModeTCP, Host: "192.168.1.100"}
	if got := tcp.connectionArgs(); !reflect.DeepEqual(got, []string{"--host", "192.168.1.100"}) {
		t.Errorf("tcp args = %v", got)
	}
	ser := Config{ConnectionMode: ModeSerial, SerialDevice: "/dev/ttyUSB0"}
	if got := ser.connectionArgs(); !reflect.DeepEqual(got, []string{"--port", "/dev/ttyUSB0"}) {
		t.Errorf("serial args = %v", got)
	}
}

func TestConfigRoundtrip(t *testing.T) {
	dir := t.TempDir()
	oldPath := ConfigFile
	ConfigFile = filepath.Join(dir, "config.json")
	defer func() { ConfigFile = oldPath }()

	cfg := Config{ConnectionMode: ModeTCP, Host: "10.9.8.7"}
	if err := setConfig(cfg); err != nil {
		t.Fatalf("setConfig: %v", err)
	}

	fi, err := os.Stat(ConfigFile)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Errorf("config file mode = %v, want 0600", fi.Mode().Perm())
	}

	gConfig = defaultConfig()
	loadConfig()
	if got := getConfig(); got != cfg {
		t.Errorf("roundtrip = %+v, want %+v", got, cfg)
	}

	// invalid configs must never hit disk
	if err := setConfig(Config{ConnectionMode: ModeTCP, Host: "8.8.8.8"}); err == nil {
		t.Error("expected setConfig to reject a public IP")
	}
	loadConfig()
	if got := getConfig(); got != cfg {
		t.Errorf("config changed after rejected set: %+v", got)
	}
}
