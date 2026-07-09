package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// Meshtastic text payloads top out around 230 bytes; keep a safe margin.
const maxMessageBytes = 200

const maxLogEntries = 200

// destRe: either the "!hexid" node id form or a decimal node number.
var destRe = regexp.MustCompile(`^(![0-9a-fA-F]{8}|[0-9]{1,10})$`)

// MessageRequest is the POST /message body.
type MessageRequest struct {
	// To is a node id ("!abcd1234") or decimal node number; empty = broadcast.
	To string
	// Channel is the channel index 0-7 (default 0).
	Channel int
	// Text is the message. UTF-8, printable, <= 200 bytes.
	Text string
}

// validateMessage enforces the allow-list before anything reaches the CLI argv.
func validateMessage(m MessageRequest) error {
	if m.Text == "" {
		return errors.New("Text is required")
	}
	if !utf8.ValidString(m.Text) {
		return errors.New("Text must be valid UTF-8")
	}
	if len(m.Text) > maxMessageBytes {
		return fmt.Errorf("Text too long: %d bytes (max %d)", len(m.Text), maxMessageBytes)
	}
	for _, r := range m.Text {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return errors.New("Text must not contain control characters")
		}
	}
	if m.To != "" && !destRe.MatchString(m.To) {
		return errors.New("To must be a node id like !abcd1234 or a decimal node number")
	}
	if m.Channel < 0 || m.Channel > 7 {
		return errors.New("Channel must be 0-7")
	}
	return nil
}

// sendArgs builds the CLI argv tail for a validated message.
func sendArgs(m MessageRequest) []string {
	args := []string{"--sendtext", m.Text}
	if m.To != "" {
		args = append(args, "--dest", m.To)
	}
	if m.Channel != 0 {
		args = append(args, "--ch-index", strconv.Itoa(m.Channel))
	}
	return args
}

// MessageLogEntry is one row of the rolling message log.
type MessageLogEntry struct {
	Time      time.Time
	Direction string // "tx" — see README: RX capture is a documented limitation
	To        string // "" = broadcast
	Channel   int
	Text      string
	Status    string // "sent" or "failed: ..."
}

var (
	messagesMtx sync.RWMutex
	gMessages   []MessageLogEntry
)

func loadMessages() {
	messagesMtx.Lock()
	defer messagesMtx.Unlock()
	data, err := os.ReadFile(MessagesFile)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Println("[-] failed to read message log:", err)
		}
		return
	}
	var msgs []MessageLogEntry
	if err := json.Unmarshal(data, &msgs); err != nil {
		fmt.Println("[-] failed to parse message log:", err)
		return
	}
	gMessages = msgs
}

func appendMessage(entry MessageLogEntry) {
	messagesMtx.Lock()
	defer messagesMtx.Unlock()
	gMessages = append(gMessages, entry)
	if len(gMessages) > maxLogEntries {
		gMessages = gMessages[len(gMessages)-maxLogEntries:]
	}
	if err := writeJSONAtomic(MessagesFile, gMessages); err != nil {
		fmt.Println("[-] failed to persist message log:", err)
	}
}

// getMessages returns the log newest-first.
func getMessages() []MessageLogEntry {
	messagesMtx.RLock()
	defer messagesMtx.RUnlock()
	out := make([]MessageLogEntry, len(gMessages))
	for i, m := range gMessages {
		out[len(gMessages)-1-i] = m
	}
	return out
}
