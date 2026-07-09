package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

var TEST_PREFIX = os.Getenv("TEST_PREFIX")

var ConfigFile = TEST_PREFIX + "/configs/spr-dnscrypt/config.json"
var TomlFile = TEST_PREFIX + "/state/plugins/spr-dnscrypt/dnscrypt-proxy.toml"
var ResolversCacheFile = "/state/plugins/spr-dnscrypt/public-resolvers.md"

// Upstream minisign public key used by dnscrypt-proxy to verify resolver list
// downloads (https://github.com/DNSCrypt/dnscrypt-resolvers).
const ResolversMinisignKey = "RWQf6LRCGA9i53mlYecO4IzT51TGPpvWucNSCh1CBM0QTaLn73Y7GFO3"

var resolversListURLs = []string{
	"https://raw.githubusercontent.com/DNSCrypt/dnscrypt-resolvers/master/v3/public-resolvers.md",
	"https://download.dnscrypt.info/resolvers-list/v3/public-resolvers.md",
}

type Config struct {
	// ServerNames is an allowlist of resolver names from the public-resolvers
	// list. Empty means "use every resolver that matches the require_* and
	// protocol filters" (dnscrypt-proxy's default behavior).
	ServerNames     []string
	RequireDNSSEC   bool
	RequireNoLog    bool
	RequireNoFilter bool
	DNSCryptServers bool
	DoHServers      bool
	Cache           bool
	// FallbackResolver is a plain-DNS IP (optionally IP:port, default port 53)
	// used only for bootstrap: resolving DoH server hostnames and connectivity
	// probes. Written to bootstrap_resolvers/netprobe_address in the TOML.
	FallbackResolver string
}

var gConfig = defaultConfig()
var Configmtx sync.RWMutex

func defaultConfig() Config {
	return Config{
		ServerNames:      []string{},
		RequireDNSSEC:    false,
		RequireNoLog:     true,
		RequireNoFilter:  true,
		DNSCryptServers:  true,
		DoHServers:       true,
		Cache:            true,
		FallbackResolver: "9.9.9.11:53",
	}
}

// Resolver names in the public list are short tokens like "cloudflare",
// "quad9-dnscrypt-ip4-filter-pri". Allowlist keeps anything written into the
// generated TOML inert (no quotes, backslashes, newlines or spaces).
var serverNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,127}$`)

func validServerName(name string) bool {
	return serverNameRe.MatchString(name)
}

// normalizeResolverAddr validates a plain-DNS bootstrap address and returns it
// in IP:port form. Accepts "9.9.9.9", "9.9.9.9:53", "[2620:fe::11]:53".
func normalizeResolverAddr(addr string) (string, error) {
	if addr == "" {
		return "", nil
	}
	if ip := net.ParseIP(addr); ip != nil {
		return net.JoinHostPort(addr, "53"), nil
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("invalid resolver address %q", addr)
	}
	if net.ParseIP(host) == nil {
		return "", fmt.Errorf("resolver address %q: host must be an IP", addr)
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 {
		return "", fmt.Errorf("resolver address %q: bad port", addr)
	}
	return net.JoinHostPort(host, port), nil
}

// validateConfig checks user input and returns a normalized copy.
func validateConfig(c Config) (Config, error) {
	if !c.DNSCryptServers && !c.DoHServers {
		return c, fmt.Errorf("at least one of DNSCryptServers/DoHServers must be enabled")
	}

	seen := map[string]bool{}
	names := []string{}
	for _, name := range c.ServerNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if !validServerName(name) {
			return c, fmt.Errorf("invalid server name %q", name)
		}
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	c.ServerNames = names

	addr, err := normalizeResolverAddr(c.FallbackResolver)
	if err != nil {
		return c, err
	}
	if addr == "" {
		addr = defaultConfig().FallbackResolver
	}
	c.FallbackResolver = addr

	return c, nil
}

func loadConfig() error {
	Configmtx.Lock()
	defer Configmtx.Unlock()
	data, err := os.ReadFile(ConfigFile)
	if err != nil {
		// keep defaults on first start
		return err
	}
	cfg := defaultConfig()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}
	cfg, err = validateConfig(cfg)
	if err != nil {
		return err
	}
	gConfig = cfg
	return nil
}

func writeConfigLocked() error {
	data, err := json.MarshalIndent(gConfig, "", " ")
	if err != nil {
		return err
	}
	return atomicWrite(ConfigFile, data, 0600)
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func tomlBool(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func tomlStrings(items []string) string {
	quoted := make([]string, 0, len(items))
	for _, it := range items {
		quoted = append(quoted, "'"+it+"'")
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// generateTOML renders dnscrypt-proxy.toml from an already-validated Config.
// listenAddrs are IP:port pairs bound inside the container network namespace.
func generateTOML(c Config, listenAddrs []string) string {
	var b strings.Builder
	b.WriteString("# Generated by the spr-dnscrypt plugin. Do NOT edit: rewritten on every\n")
	b.WriteString("# daemon (re)start from /configs/spr-dnscrypt/config.json.\n\n")

	fmt.Fprintf(&b, "listen_addresses = %s\n", tomlStrings(listenAddrs))
	b.WriteString("max_clients = 250\n\n")

	fmt.Fprintf(&b, "ipv4_servers = true\n")
	fmt.Fprintf(&b, "ipv6_servers = false\n")
	fmt.Fprintf(&b, "dnscrypt_servers = %s\n", tomlBool(c.DNSCryptServers))
	fmt.Fprintf(&b, "doh_servers = %s\n\n", tomlBool(c.DoHServers))

	fmt.Fprintf(&b, "require_dnssec = %s\n", tomlBool(c.RequireDNSSEC))
	fmt.Fprintf(&b, "require_nolog = %s\n", tomlBool(c.RequireNoLog))
	fmt.Fprintf(&b, "require_nofilter = %s\n\n", tomlBool(c.RequireNoFilter))

	if len(c.ServerNames) > 0 {
		fmt.Fprintf(&b, "server_names = %s\n\n", tomlStrings(c.ServerNames))
	}

	fmt.Fprintf(&b, "bootstrap_resolvers = %s\n", tomlStrings([]string{c.FallbackResolver}))
	b.WriteString("ignore_system_dns = true\n")
	fmt.Fprintf(&b, "netprobe_address = '%s'\n", c.FallbackResolver)
	b.WriteString("netprobe_timeout = 60\n\n")

	b.WriteString("timeout = 5000\n")
	b.WriteString("keepalive = 30\n")
	b.WriteString("cert_refresh_delay = 240\n")
	b.WriteString("log_level = 2\n\n")

	fmt.Fprintf(&b, "cache = %s\n", tomlBool(c.Cache))
	b.WriteString("cache_size = 4096\n")
	b.WriteString("cache_min_ttl = 2400\n")
	b.WriteString("cache_max_ttl = 86400\n")
	b.WriteString("cache_neg_min_ttl = 60\n")
	b.WriteString("cache_neg_max_ttl = 600\n\n")

	b.WriteString("[sources.public-resolvers]\n")
	fmt.Fprintf(&b, "urls = %s\n", tomlStrings(resolversListURLs))
	fmt.Fprintf(&b, "cache_file = '%s'\n", ResolversCacheFile)
	fmt.Fprintf(&b, "minisign_key = '%s'\n", ResolversMinisignKey)
	b.WriteString("refresh_delay = 73\n")
	b.WriteString("prefix = ''\n")

	return b.String()
}

// writeTOML snapshots the current config and writes the daemon config file.
func writeTOML(listenAddrs []string) error {
	Configmtx.RLock()
	cfg := gConfig
	Configmtx.RUnlock()
	return atomicWrite(TomlFile, []byte(generateTOML(cfg, listenAddrs)), 0600)
}
