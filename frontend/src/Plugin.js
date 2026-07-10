import React, { useEffect, useMemo, useState } from 'react'
import {
  api,
  useAlert,
  Page,
  ListHeader,
  Card,
  SectionHeader,
  StatTile,
  StatusDot,
  Toggle,
  TextField,
  Loading,
  EmptyState,
  ModalConfirm,
  Badge,
  BadgeText,
  Box,
  Button,
  ButtonText,
  HStack,
  VStack,
  Text
} from '@spr-networks/plugin-ui'
import ResolverPicker from './components/ResolverPicker'

const PLUGIN_BASE = `/plugins/${api.pluginURI() || 'spr-dnscrypt'}`

// Client-side mirror of the backend bootstrap-resolver validation: bare IP
// (v4 or v6), IPv4:port, or [IPv6]:port. Empty falls back to the default.
const fallbackResolverError = (value) => {
  const s = (value || '').trim()
  if (!s) return null
  let host = s
  let port = null
  const bracketed = s.match(/^\[([^\]]+)\](?::(\d+))?$/)
  if (bracketed) {
    host = bracketed[1]
    port = bracketed[2] ?? null
  } else if (s.includes('.') && s.includes(':')) {
    const i = s.lastIndexOf(':')
    host = s.slice(0, i)
    port = s.slice(i + 1)
  }
  const isV4 =
    /^\d{1,3}(\.\d{1,3}){3}$/.test(host) &&
    host.split('.').every((o) => Number(o) <= 255)
  const isV6 = host.includes(':') && /^[0-9A-Fa-f:]+$/.test(host)
  if (!isV4 && !isV6) {
    return 'Must be an IP address, optionally with :port (hostnames are not allowed)'
  }
  if (port !== null && !(/^\d+$/.test(port) && Number(port) >= 1 && Number(port) <= 65535)) {
    return 'Port must be between 1 and 65535'
  }
  return null
}

const SettingToggle = ({ label, helper, value, onPress }) => (
  <HStack justifyContent="space-between" alignItems="center" space="md">
    <VStack flex={1} space="xs">
      <Text size="sm" fontWeight="$medium" color="$textLight900" sx={{ _dark: { color: '$textDark50' } }}>
        {label}
      </Text>
      {helper ? (
        <Text size="xs" color="$muted500">
          {helper}
        </Text>
      ) : null}
    </VStack>
    <Toggle value={value} onPress={onPress} label={label} />
  </HStack>
)

// THE key value on this page: the address SPR's resolver must forward to.
const UpstreamAddress = ({ address, onCopy }) => (
  <Box
    p="$4"
    borderRadius="$xl"
    borderWidth={1}
    borderColor="$primary200"
    bg="$primary50"
    sx={{ _dark: { bg: '$primary950', borderColor: '$primary800' } }}
  >
    <VStack space="sm">
      <Text
        size="2xs"
        color="$muted500"
        fontWeight="$medium"
        sx={{ '@base': { letterSpacing: 0.6, textTransform: 'uppercase' } }}
      >
        Point SPR's resolver here
      </Text>
      <HStack justifyContent="space-between" alignItems="center" space="md" flexWrap="wrap">
        <Text
          size="lg"
          fontWeight="$semibold"
          color="$textLight900"
          sx={{ _dark: { color: '$textDark50' }, '@base': { fontFamily: 'monospace' } }}
        >
          {address || '—'}
        </Text>
        <Button size="xs" variant="outline" action="secondary" onPress={onCopy} isDisabled={!address}>
          <ButtonText>Copy</ButtonText>
        </Button>
      </HStack>
      <Text size="xs" color="$muted500">
        {address
          ? 'In the SPR UI, set DNS → DNS Settings → upstream DNS server to this address so every lookup leaves the router encrypted.'
          : 'Start the daemon to get the container address, then set it as the upstream DNS server under DNS → DNS Settings.'}
      </Text>
    </VStack>
  </Box>
)

export default function Plugin() {
  const alert = useAlert()
  const [status, setStatus] = useState(null)
  const [config, setConfig] = useState(null)
  const [savedConfig, setSavedConfig] = useState(null)
  const [resolvers, setResolvers] = useState([])
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(null) // null | 'save' | 'restart'
  const [confirmRestart, setConfirmRestart] = useState(false)

  const refreshStatus = () =>
    api
      .get(`${PLUGIN_BASE}/status`)
      .then(setStatus)
      .catch(() => {}) // transient poll errors stay quiet; load errors are surfaced below

  const refresh = () => {
    setLoading(true)
    Promise.all([
      api.get(`${PLUGIN_BASE}/status`),
      api.get(`${PLUGIN_BASE}/config`),
      api.get(`${PLUGIN_BASE}/resolvers`)
    ])
      .then(([st, cfg, res]) => {
        setStatus(st)
        setConfig(cfg)
        setSavedConfig(cfg)
        setResolvers(res)
      })
      .catch((err) => alert.error('Failed to load plugin data', err))
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    refresh()
    const t = setInterval(refreshStatus, 10000)
    return () => clearInterval(t)
  }, [])

  const fbError = config ? fallbackResolverError(config.FallbackResolver) : null
  const protocolError =
    config && !config.DNSCryptServers && !config.DoHServers
      ? 'Enable at least one protocol (DNSCrypt or DoH)'
      : null
  const dirty =
    config && savedConfig && JSON.stringify(config) !== JSON.stringify(savedConfig)
  const canSave = !!dirty && !fbError && !protocolError && busy === null

  const saveAndRestart = () => {
    setBusy('save')
    api
      .put(`${PLUGIN_BASE}/config`, config)
      .then((saved) => {
        // server returns the normalized config (e.g. ":53" appended)
        setConfig(saved)
        setSavedConfig(saved)
        return api.post(`${PLUGIN_BASE}/restart`, {})
      })
      .then((st) => {
        setStatus(st)
        alert.success('Saved — dnscrypt-proxy is restarting')
        // resolver probing takes a moment; refresh shortly after
        setTimeout(refreshStatus, 3000)
      })
      .catch((err) => alert.error('Failed to apply configuration', err))
      .finally(() => setBusy(null))
  }

  const restart = () => {
    setBusy('restart')
    api
      .post(`${PLUGIN_BASE}/restart`, {})
      .then((st) => {
        setStatus(st)
        alert.success('dnscrypt-proxy restarted')
        setTimeout(refreshStatus, 3000)
      })
      .catch((err) => alert.error('Failed to restart', err))
      .finally(() => setBusy(null))
  }

  const copyUpstream = (value) => {
    navigator.clipboard
      .writeText(value)
      .then(() => alert.success('Copied'))
      .catch(() => alert.error('Copy failed — select and copy the address manually'))
  }

  const running = !!status?.Running
  const ready = !!status?.Ready

  // per-resolver probe results, keyed by name (only meaningful while running)
  const health = useMemo(() => {
    if (!running) return {}
    const m = {}
    for (const r of status?.Resolvers || []) m[r.Name] = r
    return m
  }, [status, running])

  if (loading) {
    return (
      <Page>
        <Loading />
      </Page>
    )
  }

  if (!config) {
    return (
      <Page>
        <EmptyState
          title="Backend unreachable"
          description="Could not reach the spr-dnscrypt backend. Check that the plugin container is running, then retry."
        >
          <Button size="sm" onPress={refresh}>
            <ButtonText>Retry</ButtonText>
          </Button>
        </EmptyState>
      </Page>
    )
  }

  const stateWord = ready ? 'Ready' : running ? 'Starting' : 'Stopped'
  const upstream = (status?.ListenAddrs || [])[0] || ''

  return (
    <Page>
      <ListHeader
        title="DNSCrypt Proxy"
        description="Encrypted upstream DNS (DNSCrypt / DNS-over-HTTPS) for SPR"
        mark="dc"
        status={`${stateWord}${ready ? ` · ${status?.LiveServers ?? 0} live servers` : ''}`}
        statusAction={ready ? 'success' : running ? 'warning' : 'muted'}
      >
        {dirty ? (
          <Badge action="warning" variant="outline" borderRadius="$full" size="md">
            <BadgeText>Unsaved changes</BadgeText>
          </Badge>
        ) : null}
        <Button size="sm" onPress={saveAndRestart} isDisabled={!canSave}>
          <ButtonText>{busy === 'save' ? 'Applying…' : 'Save & restart'}</ButtonText>
        </Button>
      </ListHeader>

      {status?.LastError && !ready ? (
        <Card tone="warning" p="$4">
          <VStack space="xs">
            <Text size="sm" fontWeight="$semibold" color="$textLight900" sx={{ _dark: { color: '$textDark50' } }}>
              Daemon error
            </Text>
            <Text size="sm" color="$muted500" sx={{ '@base': { fontFamily: 'monospace' } }}>
              {status.LastError}
            </Text>
          </VStack>
        </Card>
      ) : null}

      <Card>
        <SectionHeader
          title="Overview"
          right={
            <HStack space="sm" alignItems="center">
              <StatusDot online={ready} warn={running && !ready} />
              <Text size="sm" color="$muted500">
                {stateWord}
              </Text>
              <Button
                size="xs"
                variant="outline"
                action="secondary"
                onPress={() => (running ? setConfirmRestart(true) : restart())}
                isDisabled={busy !== null}
              >
                <ButtonText>
                  {busy === 'restart' ? 'Restarting…' : running ? 'Restart daemon' : 'Start daemon'}
                </ButtonText>
              </Button>
            </HStack>
          }
        />
        <VStack space="lg">
          <HStack flexWrap="wrap" gap="$2">
            <StatTile label="Live servers" value={String(status?.LiveServers ?? 0)} />
            <StatTile label="Fastest resolver" value={status?.FastestServer || '—'} mono />
            <StatTile label="Uptime" value={running ? status?.Uptime || '—' : '—'} mono />
            <StatTile label="dnscrypt-proxy" value={status?.Version || '—'} mono />
          </HStack>
          <UpstreamAddress address={upstream} onCopy={() => copyUpstream(upstream)} />
        </VStack>
      </Card>

      <Card>
        <SectionHeader title="Requirements" />
        <VStack space="lg">
          <VStack space="md">
            <SettingToggle
              label="Require DNSSEC"
              helper="Only use resolvers that validate DNSSEC signatures on responses"
              value={!!config.RequireDNSSEC}
              onPress={() => setConfig({ ...config, RequireDNSSEC: !config.RequireDNSSEC })}
            />
            <SettingToggle
              label="Require no-log"
              helper="Only use resolvers that promise not to log your queries"
              value={!!config.RequireNoLog}
              onPress={() => setConfig({ ...config, RequireNoLog: !config.RequireNoLog })}
            />
            <SettingToggle
              label="Require no-filter"
              helper="Only use resolvers that don't block or rewrite results"
              value={!!config.RequireNoFilter}
              onPress={() => setConfig({ ...config, RequireNoFilter: !config.RequireNoFilter })}
            />
            <SettingToggle
              label="DNSCrypt resolvers"
              helper="Allow resolvers reached over the DNSCrypt protocol"
              value={!!config.DNSCryptServers}
              onPress={() => setConfig({ ...config, DNSCryptServers: !config.DNSCryptServers })}
            />
            <SettingToggle
              label="DoH resolvers"
              helper="Allow resolvers reached over DNS-over-HTTPS"
              value={!!config.DoHServers}
              onPress={() => setConfig({ ...config, DoHServers: !config.DoHServers })}
            />
            <SettingToggle
              label="DNS cache"
              helper="Answer repeated queries from dnscrypt-proxy's local cache"
              value={!!config.Cache}
              onPress={() => setConfig({ ...config, Cache: !config.Cache })}
            />
          </VStack>
          {protocolError ? (
            <Text size="xs" color="$red500">
              {protocolError}
            </Text>
          ) : null}
          <TextField
            label="Fallback (bootstrap) resolver"
            value={config.FallbackResolver || ''}
            onChangeText={(v) => setConfig({ ...config, FallbackResolver: v })}
            placeholder="9.9.9.11:53"
            helper="Plain-DNS IP[:port] used only to look up DoH server hostnames and for connectivity probes — regular queries never fall back to it"
            error={fbError}
          />
          <Text size="xs" color="$muted500">
            Changes apply on “Save & restart”: dnscrypt-proxy restarts and DNS pauses for a
            few seconds while resolvers are re-probed.
          </Text>
        </VStack>
      </Card>

      <ResolverPicker
        resolvers={resolvers}
        selected={config.ServerNames || []}
        onChange={(names) => setConfig({ ...config, ServerNames: names })}
        health={health}
      />

      <ModalConfirm
        isOpen={confirmRestart}
        onClose={() => setConfirmRestart(false)}
        onConfirm={restart}
        title="Restart dnscrypt-proxy?"
        message="DNS resolution pauses for a few seconds while the daemon restarts and re-probes resolvers."
        confirmText="Restart"
      />
    </Page>
  )
}
