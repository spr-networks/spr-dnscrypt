#!/bin/bash
# Command line install alternative to the UI
echo "Please enter your SPR path (/home/spr/super/)"
read -r SUPERDIR

if [ -z "$SUPERDIR" ]; then
    SUPERDIR="/home/spr/super/"
fi

export SUPERDIR

echo "Please enter your SPR API token:"
read -r SPR_API_TOKEN

if [ -z "$SPR_API_TOKEN" ]; then
  echo "need api token, generate one on the auth keys page"
  exit 1
fi

mkdir -p "$SUPERDIR/configs/plugins/spr-dnscrypt"

# SPR's UI install writes this token file (InstallTokenPath); mirror it here.
echo -n "$SPR_API_TOKEN" > "$SUPERDIR/configs/plugins/spr-dnscrypt/api-token"
chmod 600 "$SUPERDIR/configs/plugins/spr-dnscrypt/api-token"

# Default plugin config (edit later from the UI): all resolvers from the list
# that are no-log + no-filter, both DNSCrypt and DoH enabled, cache on.
if [ ! -f "$SUPERDIR/configs/plugins/spr-dnscrypt/config.json" ]; then
cat > "$SUPERDIR/configs/plugins/spr-dnscrypt/config.json" <<'EOF'
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
EOF
fi

KRUN_MAC="02:53:50:52:4b:05"
KRUN_TAP="kdnscrypt0"
curl --fail-with-body --silent --show-error "http://127.0.0.1/device?identity=${KRUN_MAC}" \
  -H "Authorization: Bearer ${SPR_API_TOKEN}" -H "Content-Type: application/json" \
  -X PUT --data-raw "{\"MAC\":\"${KRUN_MAC}\",\"Name\":\"spr-dnscrypt\",\"Policies\":[\"wan\"],\"Groups\":[\"dnscrypt\"]}" >/dev/null
if ! sudo nft get element inet filter dhcp_access "{ \"${KRUN_TAP}\" . ${KRUN_MAC} }" >/dev/null 2>&1; then
  sudo nft add element inet filter dhcp_access "{ \"${KRUN_TAP}\" . ${KRUN_MAC} : accept }"
fi

docker compose -f docker-compose-kvm.yml build
docker compose -f docker-compose-kvm.yml up -d

CONTAINER_IP=
for _ in $(seq 1 30); do
  CONTAINER_IP="$(jq -r --arg mac "$KRUN_MAC" '.[$mac].RecentIP // empty' "$SUPERDIR/state/public/devices-public.json")"
  [ -n "$CONTAINER_IP" ] && break
  sleep 1
done
[ -n "$CONTAINER_IP" ] || { echo "spr-dnscrypt did not obtain an SPR DHCP lease" >&2; exit 1; }
API=127.0.0.1

# Grant the container outbound (wan) access only. SPR's dns service connects
# TO the container; the container never needs to reach LAN devices or the API.
curl "http://${API}/firewall/custom_interface" \
-H "Authorization: Bearer ${SPR_API_TOKEN}" \
-X 'PUT' \
--data-raw "{\"SrcIP\":\"${CONTAINER_IP}\",\"Interface\":\"${KRUN_TAP}\",\"Policies\":[\"wan\"],\"Groups\":[\"dnscrypt\"]}"

echo ""
echo "[+] spr-dnscrypt is up at ${CONTAINER_IP}:53 (udp+tcp)"
echo "[+] Final step: point SPR's DNS at it."
echo "    UI: DNS -> DNS Settings -> Upstream DNS Servers -> set to ${CONTAINER_IP}"
echo "    (or: edit configs/dns/Corefile.j2 upstream / use the DNS settings API)"
