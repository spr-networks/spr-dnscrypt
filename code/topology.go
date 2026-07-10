package main

import (
	"net/http"
	"sort"
	"strings"
)

// Topology mirrors the struct shapes SPR expects from plugins that declare
// "HasTopology": true (same contract as spr-tailscale). The SPR host fetches
// GET /topology and merges the plugin graph into the router topology view at
// the "root" anchor node.
type TopoNode struct {
	ID       string
	Kind     string
	Name     string
	IP       string `json:",omitempty"`
	ConnType string `json:",omitempty"`
	Online   bool
}

type TopoEdge struct {
	From  string
	To    string
	Layer string
	Kind  string
}

type Topology struct {
	Nodes []TopoNode
	Edges []TopoEdge
}

// connType maps a dnscrypt-proxy protocol label from the probe log
// ("DNSCrypt", "DoH") to the transport tag used in the topology graph.
func connType(protocol string) string {
	return strings.ToLower(protocol)
}

// buildTopology renders the topology graph from a daemon status snapshot: a
// root anchor plus one node per live (probed OK) resolver, each with an edge
// toward root on the "dns" layer. Daemon down -> root anchor only. Pure
// function over DaemonStatus so it can be tested with a fake data source.
func buildTopology(st DaemonStatus) Topology {
	topo := Topology{
		Nodes: []TopoNode{{ID: "root", ConnType: "dnscrypt", Online: true}},
		Edges: []TopoEdge{},
	}
	if !st.Running {
		return topo
	}

	for _, r := range st.Resolvers {
		// "resolver:" prefix keeps resolver IDs from ever colliding with the
		// reserved "root" anchor id.
		id := "resolver:" + r.Name
		ct := connType(r.Protocol)
		topo.Nodes = append(topo.Nodes, TopoNode{
			ID:       id,
			Kind:     "resolver",
			Name:     r.Name,
			ConnType: ct,
			Online:   true,
		})
		topo.Edges = append(topo.Edges, TopoEdge{From: id, To: "root", Layer: "dns", Kind: ct})
	}

	resolvers := topo.Nodes[1:] // keep the root anchor first
	sort.Slice(resolvers, func(i, j int) bool { return resolvers[i].Name < resolvers[j].Name })
	sort.Slice(topo.Edges, func(i, j int) bool { return topo.Edges[i].From < topo.Edges[j].From })
	return topo
}

func handleTopology(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, buildTopology(gDaemon.Status()))
}
