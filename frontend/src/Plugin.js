import React, { useEffect, useState } from 'react'
import {
  api,
  useAlert,
  Page,
  ListHeader,
  Card,
  SectionHeader,
  StatTile,
  KeyVal,
  StatusDot,
  Toggle,
  TextField,
  Loading,
  EmptyState,
  Button,
  ButtonText,
  HStack,
  VStack,
  Text
} from '@spr-networks/plugin-ui'
import ResolverPicker from './components/ResolverPicker'

const PLUGIN_BASE = `/plugins/${api.pluginURI() || 'spr-dnscrypt'}`

const SettingToggle = ({ label, helper, value, onPress }) => (
  <HStack justifyContent="space-between" alignItems="center" space="md">
    <VStack flex={1}>
      <Text size="sm">{label}</Text>
      {helper ? (
        <Text size="xs" color="$muted500">
          {helper}
        </Text>
      ) : null}
    </VStack>
    <Toggle value={value} onPress={onPress} />
  </HStack>
)

export default function Plugin() {
  const alert = useAlert()
  const [status, setStatus] = useState(null)
  const [config, setConfig] = useState(null)
  const [resolvers, setResolvers] = useState([])
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)

  const refreshStatus = () =>
    api
      .get(`${PLUGIN_BASE}/status`)
      .then(setStatus)
      .catch((err) => alert.error('Failed to load status', err))

  const refresh = () => {
    Promise.all([
      api.get(`${PLUGIN_BASE}/status`),
      api.get(`${PLUGIN_BASE}/config`),
      api.get(`${PLUGIN_BASE}/resolvers`)
    ])
      .then(([st, cfg, res]) => {
        setStatus(st)
        setConfig(cfg)
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

  const saveAndRestart = () => {
    setBusy(true)
    api
      .put(`${PLUGIN_BASE}/config`, config)
      .then(() => api.post(`${PLUGIN_BASE}/restart`, {}))
      .then((st) => {
        setStatus(st)
        alert.success('Saved — dnscrypt-proxy restarted')
        // resolver probing takes a moment; refresh shortly after
        setTimeout(refreshStatus, 3000)
      })
      .catch((err) => alert.error('Failed to apply configuration', err))
      .finally(() => setBusy(false))
  }

  const restart = () => {
    setBusy(true)
    api
      .post(`${PLUGIN_BASE}/restart`, {})
      .then((st) => {
        setStatus(st)
        alert.success('dnscrypt-proxy restarted')
        setTimeout(refreshStatus, 3000)
      })
      .catch((err) => alert.error('Failed to restart', err))
      .finally(() => setBusy(false))
  }

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
          title="Not available"
          description="Could not reach the spr-dnscrypt backend."
        >
          <Button size="sm" onPress={refresh}>
            <ButtonText>Retry</ButtonText>
          </Button>
        </EmptyState>
      </Page>
    )
  }

  const running = !!status?.Running
  const ready = !!status?.Ready
  const active = status?.Resolvers || []

  return (
    <Page>
      <ListHeader
        title="DNSCrypt Proxy"
        description="Encrypted upstream DNS (DNSCrypt / DNS-over-HTTPS) for SPR"
      >
        <Button size="sm" onPress={saveAndRestart} isDisabled={busy}>
          <ButtonText>Save & Restart</ButtonText>
        </Button>
      </ListHeader>

      <Card>
        <SectionHeader
          title="Status"
          right={<StatusDot online={running && ready} warn={running && !ready} />}
        />
        <VStack space="md">
          <HStack flexWrap="wrap" gap="$2">
            <StatTile
              label="State"
              value={running ? (ready ? 'Running' : 'Starting') : 'Stopped'}
            />
            <StatTile label="Version" value={status?.Version || '—'} mono />
            <StatTile label="Uptime" value={status?.Uptime || '—'} mono />
            <StatTile label="Live servers" value={String(status?.LiveServers ?? 0)} mono />
          </HStack>
          <KeyVal
            label="Listening on"
            value={(status?.ListenAddrs || []).join(', ') || '—'}
            mono
          />
          <KeyVal label="Fastest resolver" value={status?.FastestServer || '—'} mono />
          {status?.LastError ? (
            <Text size="xs" color="$red500">
              {status.LastError}
            </Text>
          ) : null}
          {active.length ? (
            <VStack space="xs">
              <Text size="xs" color="$muted500">
                Active resolvers
              </Text>
              {active.slice(0, 10).map((r) => (
                <KeyVal
                  key={r.Name}
                  label={r.Name}
                  value={`${r.Protocol}${r.RTTms >= 0 ? ` — ${r.RTTms}ms` : ''}`}
                  mono
                />
              ))}
            </VStack>
          ) : null}
          <HStack>
            <Button size="xs" variant="outline" onPress={restart} isDisabled={busy}>
              <ButtonText>Restart daemon</ButtonText>
            </Button>
          </HStack>
        </VStack>
      </Card>

      <Card>
        <SectionHeader title="Requirements" />
        <VStack space="md">
          <SettingToggle
            label="Require DNSSEC"
            helper="Only use resolvers that validate DNSSEC"
            value={!!config.RequireDNSSEC}
            onPress={() => setConfig({ ...config, RequireDNSSEC: !config.RequireDNSSEC })}
          />
          <SettingToggle
            label="Require no-log"
            helper="Only use resolvers that claim not to log queries"
            value={!!config.RequireNoLog}
            onPress={() => setConfig({ ...config, RequireNoLog: !config.RequireNoLog })}
          />
          <SettingToggle
            label="Require no-filter"
            helper="Only use resolvers that don't censor results"
            value={!!config.RequireNoFilter}
            onPress={() => setConfig({ ...config, RequireNoFilter: !config.RequireNoFilter })}
          />
          <SettingToggle
            label="DNSCrypt resolvers"
            helper="Allow servers speaking the DNSCrypt protocol"
            value={!!config.DNSCryptServers}
            onPress={() => setConfig({ ...config, DNSCryptServers: !config.DNSCryptServers })}
          />
          <SettingToggle
            label="DoH resolvers"
            helper="Allow servers speaking DNS-over-HTTPS"
            value={!!config.DoHServers}
            onPress={() => setConfig({ ...config, DoHServers: !config.DoHServers })}
          />
          <SettingToggle
            label="DNS cache"
            helper="Cache responses in dnscrypt-proxy"
            value={!!config.Cache}
            onPress={() => setConfig({ ...config, Cache: !config.Cache })}
          />
          <TextField
            label="Fallback (bootstrap) resolver"
            value={config.FallbackResolver || ''}
            onChangeText={(v) => setConfig({ ...config, FallbackResolver: v })}
            placeholder="9.9.9.11:53"
            helper="Plain-DNS IP[:port] used only to bootstrap DoH hostnames and network probes"
          />
        </VStack>
      </Card>

      <ResolverPicker
        resolvers={resolvers}
        selected={config.ServerNames || []}
        onChange={(names) => setConfig({ ...config, ServerNames: names })}
      />
    </Page>
  )
}
