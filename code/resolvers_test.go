package main

import (
	"os"
	"testing"
)

// Real stamps from the pinned v3/public-resolvers.md.
const stampCloudflareDoH = "sdns://AgcAAAAAAAAABzEuMC4wLjEAEmRucy5jbG91ZGZsYXJlLmNvbQovZG5zLXF1ZXJ5"
const stampAdGuardDNSCrypt = "sdns://AQMAAAAAAAAAETk0LjE0MC4xNC4xNDo1NDQzINErR_JS3PLCu_iZEIbq95zkSV2LFsigxDIuUso_OQhzIjIuZG5zY3J5cHQuZGVmYXVsdC5uczEuYWRndWFyZC5jb20"

func TestDecodeStampDoH(t *testing.T) {
	info, err := decodeStamp(stampCloudflareDoH)
	if err != nil {
		t.Fatal(err)
	}
	if info.Protocol != "DoH" {
		t.Errorf("protocol = %q, want DoH", info.Protocol)
	}
	if !info.DNSSEC || !info.NoLog || !info.NoFilter {
		t.Errorf("props = %+v, want dnssec/nolog/nofilter all true", info)
	}
	if info.Address != "1.0.0.1" {
		t.Errorf("address = %q, want 1.0.0.1", info.Address)
	}
}

func TestDecodeStampDNSCrypt(t *testing.T) {
	info, err := decodeStamp(stampAdGuardDNSCrypt)
	if err != nil {
		t.Fatal(err)
	}
	if info.Protocol != "DNSCrypt" {
		t.Errorf("protocol = %q, want DNSCrypt", info.Protocol)
	}
	// props 0x03 = DNSSEC | NoLog, filtering resolver (no NoFilter bit)
	if !info.DNSSEC || !info.NoLog || info.NoFilter {
		t.Errorf("props = %+v, want dnssec+nolog, not nofilter", info)
	}
	if info.Address != "94.140.14.14:5443" {
		t.Errorf("address = %q, want 94.140.14.14:5443", info.Address)
	}
}

func TestDecodeStampRejects(t *testing.T) {
	for _, s := range []string{"", "https://example.com", "sdns://", "sdns://AQ", "sdns://!!!not-base64!!!"} {
		if _, err := decodeStamp(s); err == nil {
			t.Errorf("expected decodeStamp(%q) to fail", s)
		}
	}
}

const sampleList = `# public-resolvers

This is an extensive list of public DNS resolvers.

To use that list, add this to the [sources] section.

--


## cloudflare

Cloudflare 1.1.1.1 public resolver.
Global anycast, non-filtering.

sdns://AgcAAAAAAAAABzEuMC4wLjEAEmRucy5jbG91ZGZsYXJlLmNvbQovZG5zLXF1ZXJ5

--

## adguard-dns

AdGuard DNS Default public resolver.
Blocks ads, trackers, phishing and malicious domains.

sdns://AQMAAAAAAAAAETk0LjE0MC4xNC4xNDo1NDQzINErR_JS3PLCu_iZEIbq95zkSV2LFsigxDIuUso_OQhzIjIuZG5zY3J5cHQuZGVmYXVsdC5uczEuYWRndWFyZC5jb20
sdns://AQMAAAAAAAAAETk0LjE0MC4xNS4xNTo1NDQzINErR_JS3PLCu_iZEIbq95zkSV2LFsigxDIuUso_OQhzIjIuZG5zY3J5cHQuZGVmYXVsdC5uczEuYWRndWFyZC5jb20

--

## broken-entry

Has no stamps so should be skipped.
`

func TestParseResolversList(t *testing.T) {
	resolvers := parseResolversList(sampleList)
	if len(resolvers) != 2 {
		t.Fatalf("expected 2 resolvers, got %d: %+v", len(resolvers), resolvers)
	}

	cf := resolvers[0]
	if cf.Name != "cloudflare" {
		t.Errorf("name = %q", cf.Name)
	}
	if len(cf.Protocols) != 1 || cf.Protocols[0] != "DoH" {
		t.Errorf("protocols = %v", cf.Protocols)
	}
	if !cf.NoFilter || !cf.NoLog || !cf.DNSSEC {
		t.Errorf("flags = %+v", cf)
	}
	if cf.Description == "" || cf.IPv6 {
		t.Errorf("unexpected description/ipv6: %+v", cf)
	}

	ag := resolvers[1]
	if ag.Name != "adguard-dns" {
		t.Errorf("name = %q", ag.Name)
	}
	if len(ag.Protocols) != 1 || ag.Protocols[0] != "DNSCrypt" {
		t.Errorf("protocols = %v", ag.Protocols)
	}
	if len(ag.Addresses) != 2 {
		t.Errorf("addresses = %v", ag.Addresses)
	}
	if ag.NoFilter {
		t.Error("adguard default is a filtering resolver")
	}

	// preamble (before the first ##) must not leak into any entry
	for _, r := range resolvers {
		if r.Name == "public-resolvers" {
			t.Error("preamble parsed as resolver")
		}
	}
}

// TestParseRealList parses the actual vendored list when present (in the
// container image, or on a dev host with TEST_PREFIX pointing at a copy).
func TestParseRealList(t *testing.T) {
	data, err := os.ReadFile(ResolversListFile)
	if err != nil {
		t.Skipf("no resolvers list at %s: %v", ResolversListFile, err)
	}
	resolvers := parseResolversList(string(data))
	if len(resolvers) < 100 {
		t.Fatalf("expected a large list, got %d entries", len(resolvers))
	}
	seen := map[string]bool{}
	for _, r := range resolvers {
		if !validServerName(r.Name) {
			t.Errorf("resolver name %q fails our own allowlist", r.Name)
		}
		if seen[r.Name] {
			t.Errorf("duplicate resolver %q", r.Name)
		}
		seen[r.Name] = true
		if len(r.Protocols) == 0 {
			t.Errorf("resolver %q has no protocols", r.Name)
		}
	}
	for _, want := range []string{"cloudflare", "quad9-dnscrypt-ip4-nofilter-pri"} {
		if !seen[want] {
			t.Errorf("expected resolver %q in the list", want)
		}
	}
}
