# spr-dnscrypt

Encrypted outbound DNS for [SPR](https://github.com/spr-networks/super) using
[dnscrypt-proxy](https://github.com/DNSCrypt/dnscrypt-proxy). The plugin runs
dnscrypt-proxy in its own container on a dedicated docker bridge and exposes a
small UI (embedded as an iframe under SPR's Plugins menu) to pick resolvers and
tune privacy requirements. SPR's built-in CoreDNS forwards to it, so every
upstream lookup leaves the router over DNSCrypt or DNS-over-HTTPS instead of
plain UDP/53.

## Features

- dnscrypt-proxy built **from source** at a release commit pinned by full hash
  (reproducible image, see below)
- Listens on the container IP `:53` (udp+tcp) on the plugin's own bridge
  `spr-dnscrypt` — no host ports, nothing exposed on the WAN or LAN
- Resolver picker backed by the official
  [public-resolvers](https://github.com/DNSCrypt/dnscrypt-resolvers) list,
  vendored into the image (pinned by sha256) so it works offline
- Filters: DNSCrypt / DoH protocol toggles, require DNSSEC, require no-log,
  require no-filter, DNS cache on/off, fallback (bootstrap) resolver
- Status card: daemon state, uptime, dnscrypt-proxy version, live server count,
  fastest resolver, per-resolver RTT
- Save + restart from the UI; the daemon is supervised and auto-restarts with
  backoff if it crashes

## How it integrates with SPR

The plugin backend serves its REST API and the bundled UI over the unix socket
`/state/plugins/spr-dnscrypt/socket`; SPR proxies `/plugins/spr-dnscrypt/...`
to it and embeds the UI as an iframe. The dnscrypt-proxy daemon itself is only
reachable at the container's IP on the `spr-dnscrypt` bridge.

Traffic flow: `LAN clients → SPR CoreDNS (dns service) → spr-dnscrypt
container IP:53 → encrypted (DNSCrypt/DoH) → public resolvers`.

The container has the `wan` policy only (outbound internet). SPR's dns service
connects **to** the container across the docker bridges; the container never
initiates connections to the LAN or the SPR API.

### Pointing SPR's DNS at the plugin (required final step)

1. Find the container IP: it is shown in the plugin UI status card
   ("Listening on"), or run
   `docker inspect --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' spr-dnscrypt`
2. In the SPR UI open **DNS → DNS Settings** (the CoreDNS block/log plugin
   settings) and set the **Upstream DNS Server** / forward address to that
   container IP (port 53).
3. Verify from a LAN client: `dig example.com` still resolves, and the plugin
   status card shows queries hitting live servers. In the dnscrypt-proxy logs
   (`docker logs spr-dnscrypt`) you will see the chosen resolvers.

The container IP is stable for the lifetime of the docker network (default
subnet assigned by docker; recreate does not normally change it). If you
recreate the network and the IP changes, update the upstream setting to match.

## UI install

1. In the SPR UI go to **Plugins → + New Plugin** and add
   `https://github.com/spr-networks/spr-dnscrypt`
2. After installation finishes, open **spr-dnscrypt** at the bottom of the left
   menu
3. Pick resolvers (or leave the selection empty to use all resolvers matching
   the requirement toggles), hit **Save & Restart**
4. Point SPR's DNS upstream at the container IP (see above)

## CLI install

```bash
cd /home/spr/super/plugins/
git clone https://github.com/spr-networks/spr-dnscrypt
cd spr-dnscrypt
./install.sh
```

`install.sh` prompts for the SPR directory and an API token (generate one under
Auth → Tokens), writes the default plugin config, builds and starts the
container, registers the `spr-dnscrypt` custom interface with the `wan` policy,
and prints the container IP to configure as SPR's DNS upstream.

## API

All endpoints are served over the plugin unix socket and proxied by SPR at
`/plugins/spr-dnscrypt/`:

| Method | Path         | Description                                                                 |
| ------ | ------------ | --------------------------------------------------------------------------- |
| GET    | `/status`    | Daemon state: running/ready, uptime, version, live servers, fastest resolver, per-resolver protocol+RTT, listen addresses, last error |
| GET    | `/config`    | Current JSON config                                                          |
| PUT    | `/config`    | Validate + save JSON config (applied on next restart)                        |
| POST   | `/restart`   | Regenerate `dnscrypt-proxy.toml` from the saved config and restart the daemon |
| GET    | `/resolvers` | Parsed public-resolvers list (name, description, protocols, DNSSEC/no-log/no-filter flags, addresses) from the vendored copy — works offline |

## Configuration reference

`/configs/spr-dnscrypt/config.json` (mounted from
`configs/plugins/spr-dnscrypt/`):

```json
{
  "ServerNames": [],
  "RequireDNSSEC": false,
  "RequireNoLog": true,
  "RequireNoFilter": true,
  "DNSCryptServers": true,
  "DoHServers": true,
  "Cache": true,
  "FallbackResolver": "9.9.9.11:53"
}
```

- `ServerNames` — allowlist of resolver names from the public list. Empty =
  use every resolver matching the filters (dnscrypt-proxy default). Names are
  validated against `^[A-Za-z0-9][A-Za-z0-9._+-]{0,127}$`.
- `RequireDNSSEC` / `RequireNoLog` / `RequireNoFilter` — map to
  `require_dnssec` / `require_nolog` / `require_nofilter`.
- `DNSCryptServers` / `DoHServers` — map to `dnscrypt_servers` /
  `doh_servers`; at least one must be enabled.
- `Cache` — dnscrypt-proxy's internal DNS cache (`cache`).
- `FallbackResolver` — plain-DNS `IP[:port]` (IP required, hostnames
  rejected); written to `bootstrap_resolvers` and `netprobe_address`. Used
  only to resolve DoH server hostnames and for connectivity probes — regular
  queries never fall back to plain DNS.

The backend renders `/state/plugins/spr-dnscrypt/dnscrypt-proxy.toml` from this
JSON on every daemon (re)start; edits to the TOML are overwritten.

### Resolvers list & signature

The image vendors `v3/public-resolvers.md` **and**
`public-resolvers.md.minisig` from
[DNSCrypt/dnscrypt-resolvers](https://github.com/DNSCrypt/dnscrypt-resolvers),
pinned at build time by commit + sha256 (`RESOLVERS_*` in `reproducible.env`).
At startup they seed dnscrypt-proxy's source cache so the daemon can start with
no network. When online, dnscrypt-proxy refreshes the list itself and verifies
every download (and the cached copy) against the upstream minisign public key
`RWQf6LRCGA9i53mlYecO4IzT51TGPpvWucNSCh1CBM0QTaLn73Y7GFO3` — a corrupted or
tampered list is rejected.

## Security model

- **No published host ports.** The backend listens only on a unix socket; the
  DNS listener binds the container IP (plus `127.0.0.1`) inside the container's
  own network namespace on the dedicated `spr-dnscrypt` bridge.
- **No extra capabilities.** dnscrypt-proxy is a plain UDP/TCP client+listener:
  no `NET_ADMIN`, no `NET_RAW`, no devices, no sysctls. Every service runs with
  `no-new-privileges:true`.
- **Least-privilege policy:** the container interface gets only the `wan`
  policy (outbound). It cannot reach LAN devices; SPR's dns service connects to
  it, not the other way around. The plugin does not call the SPR API (no
  `ScopedPaths`).
- **Secrets: none.** The config contains no keys or passwords.
- Host mounts are limited to the plugin's own state dir (rw), its config dir
  (rw) and `configs/base/config.sh` (ro).
- All user input is validated server-side (resolver-name allowlist regex,
  IP-literal check for the bootstrap resolver) before being written into the
  generated TOML.

## Reproducible builds

Every build input is pinned in [`reproducible.env`](reproducible.env): base
image digests, the Ubuntu snapshot timestamp, the Go toolchain (version +
sha256 per arch), the dnscrypt-proxy release (version + **full commit hash** of
the release tag) and the resolvers list (commit + sha256 of the `.md` and
`.minisig`). Build with:

```bash
./build_docker_compose.sh
```

which loads the pins, sets `SOURCE_DATE_EPOCH=0` and forces the
`rewrite-timestamp` image exporter so local and CI builds produce identical
digests. Refresh all pins (new dnscrypt-proxy release, new resolvers list, new
base digests) with:

```bash
./update-pins.sh   # then review with: git diff
```

## Upstream

- [DNSCrypt/dnscrypt-proxy](https://github.com/DNSCrypt/dnscrypt-proxy) — ISC
  license. This plugin builds the unmodified upstream source; the plugin's own
  code is MIT (see [LICENSE](LICENSE)).
- [DNSCrypt/dnscrypt-resolvers](https://github.com/DNSCrypt/dnscrypt-resolvers)
  — public resolvers list, verified via minisign.
