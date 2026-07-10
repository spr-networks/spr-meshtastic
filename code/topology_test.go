package main

import (
	"reflect"
	"testing"
	"time"
)

func i64(v int64) *int64 { return &v }

func TestBuildTopologyNilInfo(t *testing.T) {
	topo := buildTopology(nil, time.Now())

	want := Topology{
		Nodes: []TopoNode{{ID: "root", ConnType: "lora", Online: true}},
		Edges: []TopoEdge{},
	}
	if !reflect.DeepEqual(topo, want) {
		t.Fatalf("nil info topology = %+v, want %+v", topo, want)
	}
}

func TestBuildTopologyGraph(t *testing.T) {
	now := time.Unix(1_719_200_000, 0)
	info := &MeshInfo{
		Owner:     "Base Station",
		MyNodeNum: 100,
		Nodes: map[string]MeshNode{
			"!00000064": {
				Num:       100,
				User:      NodeUser{ID: "!00000064", LongName: "Base Station", ShortName: "BASE"},
				LastHeard: i64(now.Unix()),
			},
			"!0000c8c8": {
				Num:       200,
				User:      NodeUser{ID: "!0000c8c8", LongName: "Trail Node", ShortName: "TRL"},
				LastHeard: i64(now.Add(-10 * time.Minute).Unix()),
			},
			"!0000d2d2": {
				Num:       300,
				User:      NodeUser{ID: "!0000d2d2", ShortName: "OLD"}, // no long name -> short name
				LastHeard: i64(now.Add(-3 * time.Hour).Unix()),         // stale -> offline
			},
			"!0000dcdc": {
				Num:  400,
				User: NodeUser{ID: "!0000dcdc"}, // never heard, no names -> id, offline
			},
		},
	}

	topo := buildTopology(info, now)

	wantNodes := []TopoNode{
		{ID: "root", ConnType: "lora", Online: true},
		{ID: "!00000064", Kind: "gateway", Name: "Base Station", ConnType: "lora", Online: true},
		// peers follow sortedNodes order: most recently heard first
		{ID: "!0000c8c8", Kind: "node", Name: "Trail Node", ConnType: "lora", Online: true},
		{ID: "!0000d2d2", Kind: "node", Name: "OLD", ConnType: "lora", Online: false},
		{ID: "!0000dcdc", Kind: "node", Name: "!0000dcdc", ConnType: "lora", Online: false},
	}
	if !reflect.DeepEqual(topo.Nodes, wantNodes) {
		t.Errorf("nodes = %+v, want %+v", topo.Nodes, wantNodes)
	}

	wantEdges := []TopoEdge{
		{From: "!00000064", To: "root", Layer: "lora", Kind: "lora"},
		{From: "!0000c8c8", To: "!00000064", Layer: "lora", Kind: "lora"},
		{From: "!0000d2d2", To: "!00000064", Layer: "lora", Kind: "lora"},
		{From: "!0000dcdc", To: "!00000064", Layer: "lora", Kind: "lora"},
	}
	if !reflect.DeepEqual(topo.Edges, wantEdges) {
		t.Errorf("edges = %+v, want %+v", topo.Edges, wantEdges)
	}
}

func TestBuildTopologyOnlineWindowBoundary(t *testing.T) {
	now := time.Unix(1_719_200_000, 0)
	info := &MeshInfo{
		MyNodeNum: 1,
		Nodes: map[string]MeshNode{
			"!00000001": {Num: 1, User: NodeUser{ID: "!00000001", LongName: "GW"}},
			"!00000002": {
				Num:       2,
				User:      NodeUser{ID: "!00000002", LongName: "Edge"},
				LastHeard: i64(now.Add(-topoOnlineWindow).Unix()), // exactly at the window
			},
		},
	}

	topo := buildTopology(info, now)
	if len(topo.Nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %+v", topo.Nodes)
	}
	peer := topo.Nodes[2]
	if peer.ID != "!00000002" || !peer.Online {
		t.Errorf("peer heard exactly %v ago should be online, got %+v", topoOnlineWindow, peer)
	}
}

func TestBuildTopologyGatewayFallbacks(t *testing.T) {
	// No self entry in the node DB: fall back to a synthetic gateway id and
	// the owner name; skip nodes without an id.
	info := &MeshInfo{
		Owner:     "Roof Antenna",
		MyNodeNum: 999,
		Nodes: map[string]MeshNode{
			"!aabbccdd": {Num: 5, User: NodeUser{ID: "!aabbccdd", LongName: "Peer"}},
			"anon":      {Num: 6, User: NodeUser{}}, // no id -> skipped
		},
	}

	topo := buildTopology(info, time.Now())

	if len(topo.Nodes) != 3 {
		t.Fatalf("expected root+gateway+1 peer, got %+v", topo.Nodes)
	}
	gw := topo.Nodes[1]
	if gw.ID != "gateway" || gw.Kind != "gateway" || gw.Name != "Roof Antenna" || !gw.Online {
		t.Errorf("gateway = %+v", gw)
	}
	if topo.Edges[0] != (TopoEdge{From: "gateway", To: "root", Layer: "lora", Kind: "lora"}) {
		t.Errorf("gateway edge = %+v", topo.Edges[0])
	}
	if topo.Edges[1] != (TopoEdge{From: "!aabbccdd", To: "gateway", Layer: "lora", Kind: "lora"}) {
		t.Errorf("peer edge = %+v", topo.Edges[1])
	}

	// Empty node DB and no owner: generic gateway name.
	topo = buildTopology(&MeshInfo{Nodes: map[string]MeshNode{}}, time.Now())
	if topo.Nodes[1].Name != "Meshtastic node" {
		t.Errorf("fallback gateway name = %q, want %q", topo.Nodes[1].Name, "Meshtastic node")
	}
}
