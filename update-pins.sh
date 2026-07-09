#!/usr/bin/env bash
# update-pins.sh — re-resolve every pin, rewrite reproducible.env, and sync the
# matching `ARG <KEY>=` defaults / `# syntax=` line in the Dockerfile(s). Read-only
# lookups (registry manifest inspect, go.dev, github.com). Review with `git diff`.
# Tip: `docker login` first to avoid Docker Hub's unauthenticated pull-rate-limit (429).
set -euo pipefail
cd "$(dirname "$0")"

# ---- tracked refs (edit to bump, then re-run) ----
UBUNTU_TAG=ubuntu:24.04
ALPINE_TAG=alpine:latest
NODE_TAG=node:18
DOCKERFILE_TAG=docker/dockerfile:1
BUILDKIT_TAG=moby/buildkit:buildx-stable-1
CONTAINER_TEMPLATE_TAG=ghcr.io/spr-networks/container_template:latest
GO_MINOR=1.25
DNSCRYPT_REPO=https://github.com/DNSCrypt/dnscrypt-proxy
RESOLVERS_REPO=https://github.com/DNSCrypt/dnscrypt-resolvers
RESOLVERS_RAW=https://raw.githubusercontent.com/DNSCrypt/dnscrypt-resolvers

mdigest() { docker buildx imagetools inspect "$1" --format '{{.Manifest.Digest}}'; }
sha256() { python3 -c 'import sys,hashlib; print(hashlib.sha256(sys.stdin.buffer.read()).hexdigest())'; }

echo "Resolving pins..." >&2
UBUNTU_REF="${UBUNTU_TAG}@$(mdigest "$UBUNTU_TAG")"
ALPINE_REF="${ALPINE_TAG%%:*}@$(mdigest "$ALPINE_TAG")"
NODE_REF="${NODE_TAG}@$(mdigest "$NODE_TAG")"
DOCKERFILE_SYNTAX="${DOCKERFILE_TAG}@$(mdigest "$DOCKERFILE_TAG")"
BUILDKIT_REF="${BUILDKIT_TAG}@$(mdigest "$BUILDKIT_TAG")"
CONTAINER_TEMPLATE_REF="${CONTAINER_TEMPLATE_TAG%:*}@$(mdigest "$CONTAINER_TEMPLATE_TAG")"
UBUNTU_SNAPSHOT="${UBUNTU_SNAPSHOT:-$(grep -E '^UBUNTU_SNAPSHOT=' reproducible.env | cut -d= -f2)}"
code=$(curl -fsS -o /dev/null -w '%{http_code}' "https://snapshot.ubuntu.com/ubuntu/${UBUNTU_SNAPSHOT}/dists/noble/InRelease" || true)
[ "$code" = "200" ] || { echo "snapshot ${UBUNTU_SNAPSHOT} not valid (HTTP $code)" >&2; exit 1; }
read -r GO_VERSION GO_SHA256_AMD64 GO_SHA256_ARM64 < <(
  curl -fsSL "https://go.dev/dl/?mode=json&include=all" | python3 -c '
import json,sys
gm=sys.argv[1]
vs=[v for v in json.load(sys.stdin) if v["version"].startswith("go"+gm+".")]
key=lambda v:[int(x) for x in (v["version"][2:].split(".")+["0","0"])[:3] if x.isdigit()]
v=sorted(vs,key=key)[-1]
sha={f["arch"]:f["sha256"] for f in v["files"] if f["os"]=="linux" and f["kind"]=="archive"}
print(v["version"][2:], sha["amd64"], sha["arm64"])' "$GO_MINOR")

# dnscrypt-proxy: latest stable release tag, resolved to the full commit hash
# the tag points at (dereference annotated tags via ^{}).
echo "Resolving dnscrypt-proxy release..." >&2
DNSCRYPT_VERSION=$(curl -fsSL "https://api.github.com/repos/DNSCrypt/dnscrypt-proxy/releases/latest" \
  | python3 -c 'import json,sys; d=json.load(sys.stdin); assert not d["prerelease"], "latest release is a prerelease"; print(d["tag_name"])')
DNSCRYPT_COMMIT=$(git ls-remote "$DNSCRYPT_REPO" "refs/tags/${DNSCRYPT_VERSION}^{}" | cut -f1)
if [ -z "$DNSCRYPT_COMMIT" ]; then
  DNSCRYPT_COMMIT=$(git ls-remote "$DNSCRYPT_REPO" "refs/tags/${DNSCRYPT_VERSION}" | cut -f1)
fi
[ -n "$DNSCRYPT_COMMIT" ] || { echo "could not resolve dnscrypt-proxy tag ${DNSCRYPT_VERSION}" >&2; exit 1; }

# Vendored public resolvers list: pin the dnscrypt-resolvers repo HEAD and the
# sha256 of v3/public-resolvers.md (+ minisign signature) at that commit.
echo "Resolving dnscrypt-resolvers list..." >&2
RESOLVERS_COMMIT=$(git ls-remote "$RESOLVERS_REPO" HEAD | cut -f1)
[ -n "$RESOLVERS_COMMIT" ] || { echo "could not resolve dnscrypt-resolvers HEAD" >&2; exit 1; }
RESOLVERS_SHA256=$(curl -fsSL "${RESOLVERS_RAW}/${RESOLVERS_COMMIT}/v3/public-resolvers.md" | sha256)
RESOLVERS_MINISIG_SHA256=$(curl -fsSL "${RESOLVERS_RAW}/${RESOLVERS_COMMIT}/v3/public-resolvers.md.minisig" | sha256)

echo "Writing reproducible.env" >&2
cat > reproducible.env <<EOF
# Pinned build inputs for build_docker_compose.sh and CI. Regenerate with ./update-pins.sh.
UBUNTU_REF=${UBUNTU_REF}
ALPINE_REF=${ALPINE_REF}
NODE_REF=${NODE_REF}
DOCKERFILE_SYNTAX=${DOCKERFILE_SYNTAX}
BUILDKIT_REF=${BUILDKIT_REF}
CONTAINER_TEMPLATE_REF=${CONTAINER_TEMPLATE_REF}
UBUNTU_SNAPSHOT=${UBUNTU_SNAPSHOT}
GO_VERSION=${GO_VERSION}
GO_SHA256_AMD64=${GO_SHA256_AMD64}
GO_SHA256_ARM64=${GO_SHA256_ARM64}
DNSCRYPT_VERSION=${DNSCRYPT_VERSION}
DNSCRYPT_COMMIT=${DNSCRYPT_COMMIT}
RESOLVERS_COMMIT=${RESOLVERS_COMMIT}
RESOLVERS_SHA256=${RESOLVERS_SHA256}
RESOLVERS_MINISIG_SHA256=${RESOLVERS_MINISIG_SHA256}
EOF

echo "Syncing Dockerfile ARG defaults + # syntax= lines" >&2
DOCKERFILES=()
while IFS= read -r f; do DOCKERFILES+=("$f"); done < <(find . -path ./node_modules -prune -o -type f -name 'Dockerfile*' -print)
replace_line() {  # <file> <sed-pattern> <new-line>  (sed: no @/$ interpolation)
  local f="$1" pat="$2" new="$3" tmp; tmp=$(mktemp)
  sed "s|${pat}|${new}|" "$f" > "$tmp" && mv "$tmp" "$f"
}
while IFS='=' read -r k v; do
  case "$k" in ''|\#*) continue;; esac
  for f in "${DOCKERFILES[@]}"; do
    if [ "$k" = "DOCKERFILE_SYNTAX" ]; then
      replace_line "$f" '^# syntax=.*' "# syntax=${v}"
    else
      replace_line "$f" "^ARG ${k}=.*" "ARG ${k}=${v}"
    fi
  done
done < reproducible.env

echo "Done. Review with: git diff" >&2
