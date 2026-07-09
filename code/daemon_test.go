package main

import (
	"testing"
)

func TestParseLine(t *testing.T) {
	d := NewDaemon()

	lines := []string{
		"[2026-07-09 10:00:01] [NOTICE] dnscrypt-proxy 2.1.16",
		"[2026-07-09 10:00:02] [NOTICE] Network connectivity detected",
		"[2026-07-09 10:00:03] [NOTICE] [cloudflare] OK (DoH) - rtt: 12ms",
		"[2026-07-09 10:00:03] [NOTICE] [quad9-dnscrypt-ip4-nofilter-pri] OK (DNSCrypt) - rtt: 35ms",
		"[2026-07-09 10:00:04] [NOTICE] Server with the lowest initial latency: cloudflare (rtt: 12ms)",
		"[2026-07-09 10:00:04] [NOTICE] dnscrypt-proxy is ready - live servers: 2",
	}
	for _, l := range lines {
		d.parseLine(0, l)
	}

	st := d.Status()
	if !st.Ready {
		t.Error("expected ready after ready line")
	}
	if st.LiveServers != 2 {
		t.Errorf("live servers = %d, want 2", st.LiveServers)
	}
	if st.FastestServer != "cloudflare" {
		t.Errorf("fastest = %q", st.FastestServer)
	}
	if len(st.Resolvers) != 2 {
		t.Fatalf("resolvers = %+v", st.Resolvers)
	}
	// sorted fastest-first
	if st.Resolvers[0].Name != "cloudflare" || st.Resolvers[0].RTTms != 12 || st.Resolvers[0].Protocol != "DoH" {
		t.Errorf("first resolver = %+v", st.Resolvers[0])
	}
	if st.Resolvers[1].Name != "quad9-dnscrypt-ip4-nofilter-pri" || st.Resolvers[1].RTTms != 35 {
		t.Errorf("second resolver = %+v", st.Resolvers[1])
	}

	d.parseLine(0, "[2026-07-09 10:01:00] [ERROR] this connection failed badly")
	if d.Status().LastError != "this connection failed badly" {
		t.Errorf("lastError = %q", d.Status().LastError)
	}

	// stale generation lines must be ignored
	d.parseLine(99, "[2026-07-09 10:02:00] [NOTICE] dnscrypt-proxy is ready - live servers: 50")
	if d.Status().LiveServers == 50 {
		t.Error("stale generation line was applied")
	}
}
