package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidServerName(t *testing.T) {
	good := []string{
		"cloudflare",
		"quad9-dnscrypt-ip4-filter-pri",
		"a-and-a-ipv6",
		"dns.sb",
		"plus+name",
		"CIRA-family",
	}
	for _, name := range good {
		if !validServerName(name) {
			t.Errorf("expected %q to be valid", name)
		}
	}

	bad := []string{
		"",
		"-leadingdash",
		".leadingdot",
		"has space",
		"quote'inject",
		"tick`x",
		"semi;colon",
		"new\nline",
		"back\\slash",
		"a']\nserver_names = ['evil",
		strings.Repeat("a", 200),
	}
	for _, name := range bad {
		if validServerName(name) {
			t.Errorf("expected %q to be rejected", name)
		}
	}
}

func TestNormalizeResolverAddr(t *testing.T) {
	cases := []struct {
		in, out string
		ok      bool
	}{
		{"", "", true},
		{"9.9.9.9", "9.9.9.9:53", true},
		{"9.9.9.11:5353", "9.9.9.11:5353", true},
		{"[2620:fe::11]:53", "[2620:fe::11]:53", true},
		{"2620:fe::11", "[2620:fe::11]:53", true},
		{"dns.example.com:53", "", false}, // hostnames not allowed (bootstrap must be an IP)
		{"9.9.9.9:0", "", false},
		{"9.9.9.9:99999", "", false},
		{"9.9.9.9:53; rm -rf /", "", false},
		{"'inject':53", "", false},
	}
	for _, c := range cases {
		out, err := normalizeResolverAddr(c.in)
		if c.ok && (err != nil || out != c.out) {
			t.Errorf("normalizeResolverAddr(%q) = %q, %v; want %q", c.in, out, err, c.out)
		}
		if !c.ok && err == nil {
			t.Errorf("normalizeResolverAddr(%q) should fail, got %q", c.in, out)
		}
	}
}

func TestValidateConfig(t *testing.T) {
	c := defaultConfig()
	c.ServerNames = []string{"cloudflare", " cloudflare ", "quad9-doh-ip4-port443-nofilter-pri"}
	c.FallbackResolver = "1.1.1.1"
	out, err := validateConfig(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.ServerNames) != 2 {
		t.Errorf("expected dedup to 2 names, got %v", out.ServerNames)
	}
	if out.FallbackResolver != "1.1.1.1:53" {
		t.Errorf("expected normalized fallback, got %q", out.FallbackResolver)
	}

	c = defaultConfig()
	c.FallbackResolver = ""
	out, err = validateConfig(c)
	if err != nil {
		t.Fatal(err)
	}
	if out.FallbackResolver != "9.9.9.11:53" {
		t.Errorf("expected default fallback, got %q", out.FallbackResolver)
	}

	c = defaultConfig()
	c.ServerNames = []string{"bad'name"}
	if _, err := validateConfig(c); err == nil {
		t.Error("expected invalid server name to be rejected")
	}

	c = defaultConfig()
	c.DNSCryptServers = false
	c.DoHServers = false
	if _, err := validateConfig(c); err == nil {
		t.Error("expected all-protocols-disabled to be rejected")
	}
}

func TestGenerateTOML(t *testing.T) {
	c := defaultConfig()
	c.ServerNames = []string{"cloudflare", "quad9-dnscrypt-ip4-nofilter-pri"}
	c.RequireDNSSEC = true
	c.Cache = false
	c.DoHServers = false
	c, err := validateConfig(c)
	if err != nil {
		t.Fatal(err)
	}

	toml := generateTOML(c, []string{"10.99.0.2:53", "127.0.0.1:53"})

	for _, want := range []string{
		"listen_addresses = ['10.99.0.2:53', '127.0.0.1:53']",
		"server_names = ['cloudflare', 'quad9-dnscrypt-ip4-nofilter-pri']",
		"require_dnssec = true",
		"require_nolog = true",
		"require_nofilter = true",
		"dnscrypt_servers = true",
		"doh_servers = false",
		"cache = false",
		"bootstrap_resolvers = ['9.9.9.11:53']",
		"netprobe_address = '9.9.9.11:53'",
		"[sources.public-resolvers]",
		"cache_file = '/state/plugins/spr-dnscrypt/public-resolvers.md'",
		"minisign_key = '" + ResolversMinisignKey + "'",
	} {
		if !strings.Contains(toml, want) {
			t.Errorf("generated TOML missing %q\n%s", want, toml)
		}
	}

	// empty allowlist must omit server_names entirely (= all servers)
	c2, _ := validateConfig(defaultConfig())
	toml2 := generateTOML(c2, []string{"127.0.0.1:53"})
	if strings.Contains(toml2, "server_names") {
		t.Error("empty ServerNames should omit server_names")
	}
}

func TestConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	oldConfig := ConfigFile
	ConfigFile = filepath.Join(dir, "config.json")
	defer func() { ConfigFile = oldConfig }()

	Configmtx.Lock()
	saved := gConfig
	gConfig = defaultConfig()
	gConfig.ServerNames = []string{"cloudflare"}
	gConfig.RequireDNSSEC = true
	err := writeConfigLocked()
	Configmtx.Unlock()
	if err != nil {
		t.Fatal(err)
	}

	fi, err := os.Stat(ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Errorf("config file mode = %v, want 0600", fi.Mode().Perm())
	}

	Configmtx.Lock()
	gConfig = Config{}
	Configmtx.Unlock()

	if err := loadConfig(); err != nil {
		t.Fatal(err)
	}
	Configmtx.RLock()
	got := gConfig
	Configmtx.RUnlock()
	if !got.RequireDNSSEC || len(got.ServerNames) != 1 || got.ServerNames[0] != "cloudflare" {
		b, _ := json.Marshal(got)
		t.Errorf("round trip mismatch: %s", b)
	}

	Configmtx.Lock()
	gConfig = saved
	Configmtx.Unlock()
}

func TestLoadConfigRejectsInvalid(t *testing.T) {
	dir := t.TempDir()
	oldConfig := ConfigFile
	ConfigFile = filepath.Join(dir, "config.json")
	defer func() { ConfigFile = oldConfig }()

	if err := os.WriteFile(ConfigFile, []byte(`{"ServerNames":["bad'name"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := loadConfig(); err == nil {
		t.Error("expected loadConfig to reject invalid server name")
	}
}
