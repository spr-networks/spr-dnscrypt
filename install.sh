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

docker compose build
docker compose up -d

CONTAINER_IP=$(docker inspect --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "spr-dnscrypt")
API=127.0.0.1

# Grant the container outbound (wan) access only. SPR's dns service connects
# TO the container; the container never needs to reach LAN devices or the API.
curl "http://${API}/firewall/custom_interface" \
-H "Authorization: Bearer ${SPR_API_TOKEN}" \
-X 'PUT' \
--data-raw "{\"SrcIP\":\"${CONTAINER_IP}\",\"Interface\":\"spr-dnscrypt\",\"Policies\":[\"wan\"],\"Groups\":[\"dnscrypt\"]}"

echo ""
echo "[+] spr-dnscrypt is up at ${CONTAINER_IP}:53 (udp+tcp)"
echo "[+] Final step: point SPR's DNS at it."
echo "    UI: DNS -> DNS Settings -> Upstream DNS Servers -> set to ${CONTAINER_IP}"
echo "    (or: edit configs/dns/Corefile.j2 upstream / use the DNS settings API)"
