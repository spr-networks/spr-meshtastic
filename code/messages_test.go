package main

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestValidateMessage(t *testing.T) {
	ok := []MessageRequest{
		{Text: "hello mesh"},
		{Text: "hi", To: "!abcd1234"},
		{Text: "hi", To: "862521276"},
		{Text: "hi", Channel: 7},
		{Text: "multi\nline\ttext"},
		{Text: strings.Repeat("a", 200)},
		{Text: "unicode ✓ émojis 🚀"},
	}
	for _, m := range ok {
		if err := validateMessage(m); err != nil {
			t.Errorf("expected %+v to validate, got %v", m, err)
		}
	}
	bad := []MessageRequest{
		{},
		{Text: strings.Repeat("a", 201)},
		{Text: string([]byte{0xff, 0xfe})}, // invalid UTF-8
		{Text: "bell\x07"},                 // control char
		{Text: "esc\x1b[31m"},              // ANSI escape
		{Text: "hi", To: "abcd1234"},       // missing !
		{Text: "hi", To: "!abcd123"},       // too short
		{Text: "hi", To: "!abcd12345"},     // too long
		{Text: "hi", To: "!ghijklmn"},      // not hex
		{Text: "hi", To: "12345678901"},    // > 10 digits
		{Text: "hi", To: "'; reboot"},      // junk
		{Text: "hi", Channel: -1},
		{Text: "hi", Channel: 8},
	}
	for _, m := range bad {
		if err := validateMessage(m); err == nil {
			t.Errorf("expected %+v to be rejected", m)
		}
	}
	// a 4-byte rune can push a 199-char string over the 200 byte limit
	m := MessageRequest{Text: strings.Repeat("a", 197) + "🚀"}
	if err := validateMessage(m); err == nil {
		t.Error("expected byte-length (not rune-length) enforcement")
	}
}

func TestSendArgs(t *testing.T) {
	cases := []struct {
		in   MessageRequest
		want []string
	}{
		{MessageRequest{Text: "hi"}, []string{"--sendtext", "hi"}},
		{MessageRequest{Text: "hi", To: "!abcd1234"}, []string{"--sendtext", "hi", "--dest", "!abcd1234"}},
		{MessageRequest{Text: "hi", Channel: 2}, []string{"--sendtext", "hi", "--ch-index", "2"}},
		{
			MessageRequest{Text: "hi", To: "42", Channel: 1},
			[]string{"--sendtext", "hi", "--dest", "42", "--ch-index", "1"},
		},
	}
	for _, c := range cases {
		if got := sendArgs(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("sendArgs(%+v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestMessageLogRollingAndPersistence(t *testing.T) {
	dir := t.TempDir()
	oldPath := MessagesFile
	MessagesFile = filepath.Join(dir, "messages.json")
	defer func() { MessagesFile = oldPath }()

	gMessages = nil
	for i := 0; i < maxLogEntries+10; i++ {
		appendMessage(MessageLogEntry{
			Time:      time.Now().UTC(),
			Direction: "tx",
			Text:      fmt.Sprintf("msg %d", i),
			Status:    "sent",
		})
	}

	msgs := getMessages()
	if len(msgs) != maxLogEntries {
		t.Fatalf("log length = %d, want %d", len(msgs), maxLogEntries)
	}
	// newest first
	if msgs[0].Text != fmt.Sprintf("msg %d", maxLogEntries+9) {
		t.Errorf("newest = %q", msgs[0].Text)
	}
	if msgs[len(msgs)-1].Text != "msg 10" {
		t.Errorf("oldest = %q", msgs[len(msgs)-1].Text)
	}

	// reload from state file
	gMessages = nil
	loadMessages()
	reloaded := getMessages()
	if len(reloaded) != maxLogEntries || reloaded[0].Text != msgs[0].Text {
		t.Errorf("reload mismatch: %d entries", len(reloaded))
	}
}
