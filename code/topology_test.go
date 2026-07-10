package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestBuildTopologyDaemonDown(t *testing.T) {
	// stale resolver measurements must not leak into the graph when the
	// daemon is not running
	st := DaemonStatus{
		Running: false,
		Resolvers: []ResolverHealth{
			{Name: "cloudflare", Protocol: "DoH", RTTms: 12},
		},
	}

	topo := buildTopology(st)
	if len(topo.Nodes) != 1 || len(topo.Edges) != 0 {
		t.Fatalf("daemon down: got %d nodes / %d edges, want 1 / 0", len(topo.Nodes), len(topo.Edges))
	}
	root := topo.Nodes[0]
	if root.ID != "root" || root.ConnType != "dnscrypt" || !root.Online {
		t.Errorf("root anchor = %+v", root)
	}

	// the host expects "Edges":[] (not null) in the JSON
	data, err := json.Marshal(topo)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"Edges":[]`) {
		t.Errorf("empty edges must encode as [], got %s", data)
	}
}

func TestBuildTopologyLiveResolvers(t *testing.T) {
	st := DaemonStatus{
		Running: true,
		Ready:   true,
		Resolvers: []ResolverHealth{
			{Name: "quad9-dnscrypt-ip4-nofilter-pri", Protocol: "DNSCrypt", RTTms: 35},
			{Name: "cloudflare", Protocol: "DoH", RTTms: 12},
		},
	}

	topo := buildTopology(st)
	if len(topo.Nodes) != 3 {
		t.Fatalf("nodes = %+v, want root + 2 resolvers", topo.Nodes)
	}
	if topo.Nodes[0].ID != "root" {
		t.Errorf("first node must be the root anchor, got %+v", topo.Nodes[0])
	}

	// resolver nodes sorted by name after the root anchor
	first, second := topo.Nodes[1], topo.Nodes[2]
	if first.Name != "cloudflare" || second.Name != "quad9-dnscrypt-ip4-nofilter-pri" {
		t.Errorf("resolver nodes not sorted by name: %+v", topo.Nodes[1:])
	}
	if first.ID != "resolver:cloudflare" || first.Kind != "resolver" || !first.Online {
		t.Errorf("cloudflare node = %+v", first)
	}
	if first.ConnType != "doh" || second.ConnType != "dnscrypt" {
		t.Errorf("conn types = %q / %q, want doh / dnscrypt", first.ConnType, second.ConnType)
	}
	if first.IP != "" {
		t.Errorf("IP should be omitted for resolvers, got %q", first.IP)
	}

	if len(topo.Edges) != 2 {
		t.Fatalf("edges = %+v, want 2", topo.Edges)
	}
	for _, e := range topo.Edges {
		if e.To != "root" || e.Layer != "dns" {
			t.Errorf("edge = %+v, want To=root Layer=dns", e)
		}
	}
	if topo.Edges[0].From != "resolver:cloudflare" || topo.Edges[0].Kind != "doh" {
		t.Errorf("first edge = %+v", topo.Edges[0])
	}
	if topo.Edges[1].From != "resolver:quad9-dnscrypt-ip4-nofilter-pri" || topo.Edges[1].Kind != "dnscrypt" {
		t.Errorf("second edge = %+v", topo.Edges[1])
	}
}

// End-to-end over the daemon's own log parser as the (fake) data source:
// feed probe lines, mark the daemon running, and build the graph from the
// resulting Status snapshot.
func TestBuildTopologyFromDaemonLogs(t *testing.T) {
	d := NewDaemon()
	for _, line := range []string{
		"[2026-07-09 10:00:03] [NOTICE] [cloudflare] OK (DoH) - rtt: 12ms",
		"[2026-07-09 10:00:03] [NOTICE] [adguard-dns] OK (DNSCrypt) - rtt: 44ms",
		"[2026-07-09 10:00:04] [NOTICE] dnscrypt-proxy is ready - live servers: 2",
	} {
		d.parseLine(0, line)
	}
	d.mtx.Lock()
	d.running = true
	d.startedAt = time.Now()
	d.mtx.Unlock()

	topo := buildTopology(d.Status())
	if len(topo.Nodes) != 3 || len(topo.Edges) != 2 {
		t.Fatalf("got %d nodes / %d edges, want 3 / 2", len(topo.Nodes), len(topo.Edges))
	}
	names := map[string]string{}
	for _, n := range topo.Nodes[1:] {
		names[n.Name] = n.ConnType
	}
	if names["cloudflare"] != "doh" || names["adguard-dns"] != "dnscrypt" {
		t.Errorf("resolver nodes = %+v", topo.Nodes[1:])
	}
}
