# syntax=docker/dockerfile:1@sha256:87999aa3d42bdc6bea60565083ee17e86d1f3339802f543c0d03998580f9cb89
ARG ALPINE_REF=alpine@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b
ARG UBUNTU_REF=ubuntu:24.04@sha256:4fbb8e6a8395de5a7550b33509421a2bafbc0aab6c06ba2cef9ebffbc7092d90
ARG NODE_REF=node:18@sha256:c6ae79e38498325db67193d391e6ec1d224d96c693a8a4d943498556716d3783
ARG CONTAINER_TEMPLATE_REF=ghcr.io/spr-networks/container_template@sha256:869ada7b121e9a0c552674042d32e801da3c4d04145638d9e722918c6377e65f
ARG SOURCE_DATE_EPOCH

FROM ${ALPINE_REF} AS cacerts

FROM ${UBUNTU_REF} AS builder
ENV DEBIAN_FRONTEND=noninteractive
ARG UBUNTU_SNAPSHOT=20260601T000000Z
ARG GO_VERSION=1.25.12
ARG GO_SHA256_AMD64=234828b7a89e0e303d2556310ee549fbcf253d28de937bac3da13d6294262ac1
ARG GO_SHA256_ARM64=8b5884aef89600aef5b0b051fb971f11f49bb996521e911f30f02a66884f7bd2
ARG DNSCRYPT_VERSION=2.1.16
ARG DNSCRYPT_COMMIT=140587c79df3c1edb7fe11fa2f9c135e122e584b
ARG RESOLVERS_COMMIT=8fd7a9943909462c2871ffaef7fe99b67e7db2ee
ARG RESOLVERS_SHA256=55751b5f585786677c3d7562fdbf85773a01ea87cc8681273bdaf9faa007b905
ARG RESOLVERS_MINISIG_SHA256=4a342e0656286a18b1ae8e402184978bcac00b7c873e1299d75418577cad16b8
ARG TARGETARCH
COPY --from=cacerts /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
RUN set -eux; \
    printf 'Types: deb\nURIs: https://snapshot.ubuntu.com/ubuntu/%s\nSuites: noble noble-updates noble-security\nComponents: main restricted universe multiverse\nSigned-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg\n' "${UBUNTU_SNAPSHOT}" > /etc/apt/sources.list.d/ubuntu.sources; \
    printf 'APT::Install-Recommends "false";\nAcquire::Check-Valid-Until "false";\n' > /etc/apt/apt.conf.d/99reproducible
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates git wget && rm -rf /var/lib/apt/lists/* /var/log/* /var/cache/ldconfig/aux-cache
RUN set -eux; \
    case "${TARGETARCH}" in \
      amd64) GO_SHA256="${GO_SHA256_AMD64}";; \
      arm64) GO_SHA256="${GO_SHA256_ARM64}";; \
      *) echo "unsupported TARGETARCH=${TARGETARCH}" >&2; exit 1;; \
    esac; \
    wget -q "https://dl.google.com/go/go${GO_VERSION}.linux-${TARGETARCH}.tar.gz"; \
    echo "${GO_SHA256}  go${GO_VERSION}.linux-${TARGETARCH}.tar.gz" | sha256sum -c -; \
    tar -C /usr/local -xzf "go${GO_VERSION}.linux-${TARGETARCH}.tar.gz"; \
    rm "go${GO_VERSION}.linux-${TARGETARCH}.tar.gz"
ENV PATH="/usr/local/go/bin:${PATH}" GOTOOLCHAIN=local

# dnscrypt-proxy built from source, pinned to the full commit hash of release
# tag ${DNSCRYPT_VERSION} (never a branch or tag name).
RUN set -eux; \
    git init /dnscrypt-src; \
    cd /dnscrypt-src; \
    git remote add origin https://github.com/DNSCrypt/dnscrypt-proxy; \
    git fetch --depth 1 origin "${DNSCRYPT_COMMIT}"; \
    git checkout --detach "${DNSCRYPT_COMMIT}"
RUN --mount=type=tmpfs,target=/root/go/ cd /dnscrypt-src && go build -trimpath -ldflags "-s -w" -o /dnscrypt-proxy ./dnscrypt-proxy

# Vendor the public resolvers list (and its minisign signature) pinned by
# commit + sha256 so /resolvers and first daemon start work fully offline.
RUN set -eux; \
    wget -q "https://raw.githubusercontent.com/DNSCrypt/dnscrypt-resolvers/${RESOLVERS_COMMIT}/v3/public-resolvers.md"; \
    echo "${RESOLVERS_SHA256}  public-resolvers.md" | sha256sum -c -; \
    wget -q "https://raw.githubusercontent.com/DNSCrypt/dnscrypt-resolvers/${RESOLVERS_COMMIT}/v3/public-resolvers.md.minisig"; \
    echo "${RESOLVERS_MINISIG_SHA256}  public-resolvers.md.minisig" | sha256sum -c -; \
    mkdir -p /resolvers; \
    mv public-resolvers.md public-resolvers.md.minisig /resolvers/

WORKDIR /code
COPY code/ /code/
RUN --mount=type=tmpfs,target=/root/go/ go build -trimpath -ldflags "-s -w -X main.gDNSCryptVersion=${DNSCRYPT_VERSION}" -o /dnscrypt_plugin /code/

FROM ${NODE_REF} AS builder-ui
WORKDIR /app
COPY frontend ./
RUN --mount=type=tmpfs,target=/root/.cache \
    --mount=type=tmpfs,target=/app/node_modules \
    yarn install --frozen-lockfile --network-timeout 86400000 && yarn run bundle

FROM ${CONTAINER_TEMPLATE_REF}
ENV DEBIAN_FRONTEND=noninteractive
COPY scripts /scripts/
COPY --from=builder /dnscrypt-proxy /dnscrypt-proxy
COPY --from=builder /dnscrypt_plugin /dnscrypt_plugin
COPY --from=builder /resolvers/ /usr/share/dnscrypt-proxy/
COPY --from=builder-ui /app/build/ /ui/

ENTRYPOINT ["/scripts/startup.sh"]
