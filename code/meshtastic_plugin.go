package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// ---- REST handlers (SPR proxies /plugins/spr-meshtastic/<path> here) ----

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		fmt.Println("[-] encode failed:", err)
	}
}

// StatusResponse is the GET /status payload for the UI status card.
type StatusResponse struct {
	Configured     bool
	Connected      bool
	ConnectionMode string
	Host           string `json:",omitempty"`
	SerialDevice   string `json:",omitempty"`
	Owner          string `json:",omitempty"`
	OwnerShort     string `json:",omitempty"`
	NodeID         string `json:",omitempty"`
	HwModel        string `json:",omitempty"`
	Firmware       string `json:",omitempty"`
	BatteryLevel   *int   `json:",omitempty"`
	NumNodes       int
	ChannelURL     string     `json:",omitempty"`
	LastUpdated    *time.Time `json:",omitempty"`
	Error          string     `json:",omitempty"`
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	cfg := getConfig()
	resp := StatusResponse{
		Configured:     cfg.configured(),
		ConnectionMode: cfg.ConnectionMode,
		Host:           cfg.Host,
		SerialDevice:   cfg.SerialDevice,
	}
	if !resp.Configured {
		writeJSON(w, resp)
		return
	}

	info, err := getInfo(r.URL.Query().Get("refresh") == "1")
	if err != nil {
		resp.Error = err.Error()
		writeJSON(w, resp)
		return
	}
	now := gInfoCacheFetchedAt()
	resp.Connected = true
	resp.LastUpdated = &now
	resp.Owner = info.Owner
	resp.OwnerShort = info.OwnerShort
	resp.Firmware = info.Firmware
	resp.NumNodes = len(info.Nodes)
	resp.ChannelURL = info.PrimaryURL
	if self := info.self(); self != nil {
		resp.NodeID = self.User.ID
		resp.HwModel = self.User.HwModel
		if self.DeviceMetrics != nil {
			resp.BatteryLevel = self.DeviceMetrics.BatteryLevel
		}
	}
	writeJSON(w, resp)
}

func gInfoCacheFetchedAt() time.Time {
	gInfoCache.mu.Lock()
	defer gInfoCache.mu.Unlock()
	return gInfoCache.fetchedAt
}

// NodeResponse is one row of the GET /nodes table.
type NodeResponse struct {
	ID           string
	Num          uint32
	LongName     string
	ShortName    string
	HwModel      string
	SNR          *float64 `json:",omitempty"`
	LastHeard    *int64   `json:",omitempty"`
	HopsAway     *int     `json:",omitempty"`
	BatteryLevel *int     `json:",omitempty"`
	Voltage      *float64 `json:",omitempty"`
	IsSelf       bool
}

func handleNodes(w http.ResponseWriter, r *http.Request) {
	info, err := getInfo(r.URL.Query().Get("refresh") == "1")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	nodes := []NodeResponse{}
	for _, n := range info.sortedNodes() {
		row := NodeResponse{
			ID:        n.User.ID,
			Num:       n.Num,
			LongName:  n.User.LongName,
			ShortName: n.User.ShortName,
			HwModel:   n.User.HwModel,
			SNR:       n.SNR,
			LastHeard: n.LastHeard,
			HopsAway:  n.HopsAway,
			IsSelf:    n.Num == info.MyNodeNum,
		}
		if n.DeviceMetrics != nil {
			row.BatteryLevel = n.DeviceMetrics.BatteryLevel
			row.Voltage = n.DeviceMetrics.Voltage
		}
		nodes = append(nodes, row)
	}
	writeJSON(w, nodes)
}

func handleSendMessage(w http.ResponseWriter, r *http.Request) {
	var msg MessageRequest
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if err := validateMessage(msg); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	entry := MessageLogEntry{
		Time:      time.Now().UTC(),
		Direction: "tx",
		To:        msg.To,
		Channel:   msg.Channel,
		Text:      msg.Text,
		Status:    "sent",
	}
	_, err := runMeshtastic(getConfig(), sendArgs(msg)...)
	if err != nil {
		entry.Status = "failed: " + err.Error()
		appendMessage(entry)
		http.Error(w, err.Error(), 500)
		return
	}
	appendMessage(entry)
	writeJSON(w, entry)
}

func handleMessages(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, getMessages())
}

// ConfigResponse echoes the config; there are no secrets in it.
type ConfigResponse struct {
	Config
	Configured bool
}

func handleGetConfig(w http.ResponseWriter, r *http.Request) {
	cfg := getConfig()
	writeJSON(w, ConfigResponse{Config: cfg, Configured: cfg.configured()})
}

func handlePutConfig(w http.ResponseWriter, r *http.Request) {
	var cfg Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if err := setConfig(cfg); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	invalidateInfoCache()
	writeJSON(w, ConfigResponse{Config: cfg, Configured: true})
}

// ChannelResponse is the GET /channel payload (QR/share URLs of the node).
type ChannelResponse struct {
	PrimaryURL  string `json:",omitempty"`
	CompleteURL string `json:",omitempty"`
}

func handleChannel(w http.ResponseWriter, r *http.Request) {
	info, err := getInfo(false)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, ChannelResponse{PrimaryURL: info.PrimaryURL, CompleteURL: info.CompleteURL})
}

// ---- UI + server plumbing ----

// spaHandler serves /ui (the bundled frontend); unknown paths fall back to
// index.html, which the SPR host fetches and injects as iframe srcDoc.
type spaHandler struct {
	staticPath string
	indexPath  string
}

func (h spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path, err := filepath.Abs(r.URL.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	path = filepath.Join(h.staticPath, path)
	_, err = os.Stat(path)
	if os.IsNotExist(err) {
		http.ServeFile(w, r, filepath.Join(h.staticPath, h.indexPath))
		return
	} else if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.FileServer(http.Dir(h.staticPath)).ServeHTTP(w, r)
}

func logRequest(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("%s %s %s\n", r.RemoteAddr, r.Method, r.URL)
		handler.ServeHTTP(w, r)
	})
}

func main() {
	loadConfig()
	loadMessages()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", handleStatus)
	mux.HandleFunc("GET /nodes", handleNodes)
	mux.HandleFunc("POST /message", handleSendMessage)
	mux.HandleFunc("GET /messages", handleMessages)
	mux.HandleFunc("GET /config", handleGetConfig)
	mux.HandleFunc("PUT /config", handlePutConfig)
	mux.HandleFunc("GET /channel", handleChannel)
	mux.HandleFunc("GET /topology", handleTopology)
	mux.Handle("/", spaHandler{staticPath: "/ui", indexPath: "index.html"})

	os.Remove(UnixPluginSocket)
	listener, err := net.Listen("unix", UnixPluginSocket)
	if err != nil {
		panic(err)
	}
	if err := os.Chmod(UnixPluginSocket, 0770); err != nil {
		panic(err)
	}

	server := http.Server{Handler: logRequest(mux)}
	if err := server.Serve(listener); err != nil {
		panic(err)
	}
}
