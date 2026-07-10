package main

import (
	"net/http"
	"time"
)

// Topology mirrors the SPR host topology contract (see spr-tailscale): the
// host merges the plugin's graph into the router topology at the "root"
// anchor node.

// TopoNode is one node of the plugin topology graph.
type TopoNode struct {
	ID       string
	Kind     string
	Name     string
	IP       string `json:",omitempty"`
	ConnType string `json:",omitempty"`
	Online   bool
}

// TopoEdge is one edge of the plugin topology graph.
type TopoEdge struct {
	From  string
	To    string
	Layer string
	Kind  string
}

// Topology is the GET /topology payload.
type Topology struct {
	Nodes []TopoNode
	Edges []TopoEdge
}

// topoOnlineWindow: a mesh peer heard within this window counts as online.
const topoOnlineWindow = 2 * time.Hour

// buildTopology converts mesh info into the SPR topology graph:
// root anchor <- gateway (the Meshtastic device we drive) <- one node per
// mesh peer. A nil info (unconfigured or unreachable node) yields the bare
// root anchor so the host still gets a valid, empty graph.
func buildTopology(info *MeshInfo, now time.Time) Topology {
	topo := Topology{
		Nodes: []TopoNode{{ID: "root", ConnType: "lora", Online: true}},
		Edges: []TopoEdge{},
	}
	if info == nil {
		return topo
	}

	gatewayID := "gateway"
	gatewayName := info.Owner
	if self := info.self(); self != nil && self.User.ID != "" {
		gatewayID = self.User.ID
		if gatewayName == "" {
			gatewayName = self.User.LongName
		}
	}
	if gatewayName == "" {
		gatewayName = "Meshtastic node"
	}

	topo.Nodes = append(topo.Nodes, TopoNode{
		ID:       gatewayID,
		Kind:     "gateway",
		Name:     gatewayName,
		ConnType: "lora",
		Online:   true, // info was just fetched from it
	})
	topo.Edges = append(topo.Edges, TopoEdge{From: gatewayID, To: "root", Layer: "lora", Kind: "lora"})

	for _, n := range info.sortedNodes() {
		if n.Num == info.MyNodeNum || n.User.ID == "" {
			continue
		}
		name := n.User.LongName
		if name == "" {
			name = n.User.ShortName
		}
		if name == "" {
			name = n.User.ID
		}
		online := n.LastHeard != nil && now.Sub(time.Unix(*n.LastHeard, 0)) <= topoOnlineWindow
		topo.Nodes = append(topo.Nodes, TopoNode{
			ID:       n.User.ID,
			Kind:     "node",
			Name:     name,
			ConnType: "lora",
			Online:   online,
		})
		topo.Edges = append(topo.Edges, TopoEdge{From: n.User.ID, To: gatewayID, Layer: "lora", Kind: "lora"})
	}
	return topo
}

func handleTopology(w http.ResponseWriter, r *http.Request) {
	var info *MeshInfo
	if getConfig().configured() {
		if i, err := getInfo(false); err == nil {
			info = i
		}
	}
	writeJSON(w, buildTopology(info, time.Now()))
}
