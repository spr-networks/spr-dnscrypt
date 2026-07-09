package main

import (
	"bufio"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"os"
	"strings"
)

// Vendored at image build time, pinned by sha256 (see Dockerfile /
// reproducible.env), so /resolvers works fully offline.
var ResolversListFile = TEST_PREFIX + "/usr/share/dnscrypt-proxy/public-resolvers.md"

type Resolver struct {
	Name        string
	Description string
	Protocols   []string
	Addresses   []string
	DNSSEC      bool
	NoLog       bool
	NoFilter    bool
	IPv6        bool
}

type stampInfo struct {
	Protocol string
	Address  string
	DNSSEC   bool
	NoLog    bool
	NoFilter bool
}

// decodeStamp parses a DNS stamp (sdns://...) far enough to extract the
// protocol, informal properties and the server address.
// Layout: base64url(raw), raw[0]=protocol, raw[1:9]=props (LE uint64,
// bit0 DNSSEC / bit1 NoLog / bit2 NoFilter), then len-prefixed address.
func decodeStamp(stamp string) (stampInfo, error) {
	info := stampInfo{}
	s, ok := strings.CutPrefix(stamp, "sdns://")
	if !ok {
		return info, fmt.Errorf("not a DNS stamp")
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return info, fmt.Errorf("bad stamp encoding: %w", err)
	}
	if len(raw) < 9 {
		return info, fmt.Errorf("stamp too short")
	}

	switch raw[0] {
	case 0x00:
		info.Protocol = "Plain"
	case 0x01:
		info.Protocol = "DNSCrypt"
	case 0x02:
		info.Protocol = "DoH"
	case 0x03:
		info.Protocol = "DoT"
	case 0x04:
		info.Protocol = "DoQ"
	case 0x05:
		info.Protocol = "oDoH"
	case 0x81:
		info.Protocol = "DNSCryptRelay"
	case 0x85:
		info.Protocol = "oDoHRelay"
	default:
		info.Protocol = fmt.Sprintf("unknown(0x%02x)", raw[0])
	}

	props := binary.LittleEndian.Uint64(raw[1:9])
	info.DNSSEC = props&0x01 != 0
	info.NoLog = props&0x02 != 0
	info.NoFilter = props&0x04 != 0

	// address is the first length-prefixed field after props
	if len(raw) > 9 {
		alen := int(raw[9])
		if 10+alen <= len(raw) {
			info.Address = string(raw[10 : 10+alen])
		}
	}

	return info, nil
}

// parseResolversList parses the dnscrypt public-resolvers.md format:
// "## name" headings followed by free-text description lines and one or more
// sdns:// stamps, sections separated by "--" lines.
func parseResolversList(data string) []Resolver {
	resolvers := []Resolver{}
	var cur *Resolver
	descLines := []string{}
	propsSet := false

	flush := func() {
		if cur == nil {
			return
		}
		desc := strings.Join(descLines, " ")
		if len(desc) > 400 {
			desc = desc[:400]
		}
		cur.Description = desc
		if len(cur.Protocols) > 0 {
			resolvers = append(resolvers, *cur)
		}
		cur = nil
		descLines = nil
		propsSet = false
	}

	scanner := bufio.NewScanner(strings.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if name, ok := strings.CutPrefix(line, "## "); ok {
			flush()
			name = strings.TrimSpace(name)
			if validServerName(name) {
				cur = &Resolver{Name: name, Protocols: []string{}, Addresses: []string{}}
			}
			continue
		}
		if cur == nil {
			continue
		}
		if strings.HasPrefix(line, "sdns://") {
			info, err := decodeStamp(line)
			if err != nil {
				continue
			}
			if !contains(cur.Protocols, info.Protocol) {
				cur.Protocols = append(cur.Protocols, info.Protocol)
			}
			if info.Address != "" && !contains(cur.Addresses, info.Address) {
				cur.Addresses = append(cur.Addresses, info.Address)
			}
			if strings.Contains(info.Address, "[") || strings.Count(info.Address, ":") > 1 {
				cur.IPv6 = true
			}
			// properties come from the stamps; first stamp wins
			if !propsSet {
				cur.DNSSEC = info.DNSSEC
				cur.NoLog = info.NoLog
				cur.NoFilter = info.NoFilter
				propsSet = true
			}
			continue
		}
		if line != "" && line != "--" {
			descLines = append(descLines, line)
		}
	}
	flush()

	return resolvers
}

func contains(items []string, s string) bool {
	for _, it := range items {
		if it == s {
			return true
		}
	}
	return false
}

func loadResolvers() ([]Resolver, error) {
	data, err := os.ReadFile(ResolversListFile)
	if err != nil {
		return nil, err
	}
	return parseResolversList(string(data)), nil
}
