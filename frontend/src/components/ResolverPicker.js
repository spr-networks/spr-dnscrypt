import React, { useMemo, useState } from 'react'
import {
  Badge,
  BadgeText,
  Button,
  ButtonText,
  Card,
  EmptyState,
  HStack,
  Pressable,
  SearchIcon,
  SectionHeader,
  Text,
  TextField,
  VStack
} from '@spr-networks/plugin-ui'

const MAX_SHOWN = 60

const EMPTY_FLAGS = {
  dnscrypt: false,
  doh: false,
  dnssec: false,
  nolog: false,
  nofilter: false,
  selected: false,
  live: false
}

// Small pressable pill used for the "show only ..." filters.
const FilterChip = ({ label, active, onPress }) => (
  <Pressable onPress={onPress}>
    <Badge
      size="md"
      borderRadius="$full"
      variant={active ? 'solid' : 'outline'}
      action={active ? 'info' : 'muted'}
    >
      <BadgeText>{label}</BadgeText>
    </Badge>
  </Pressable>
)

const PropertyPill = ({ label }) => (
  <Badge size="sm" variant="outline" action="muted">
    <BadgeText>{label}</BadgeText>
  </Badge>
)

const ResolverRow = ({ resolver, selected, live, onPress }) => (
  <Pressable onPress={onPress}>
    <HStack
      space="md"
      alignItems="center"
      justifyContent="space-between"
      py="$3"
      px="$3"
      borderBottomWidth={1}
      borderColor="$borderColorCardLight"
      borderRadius={selected ? '$md' : undefined}
      bg={selected ? '$primary50' : undefined}
      sx={{
        _dark: {
          borderColor: '$borderColorCardDark',
          ...(selected ? { bg: '$primary950' } : {})
        }
      }}
    >
      <VStack flex={1} space="xs">
        <HStack space="sm" alignItems="center" flexWrap="wrap">
          <Text size="sm" bold>
            {resolver.Name}
          </Text>
          {(resolver.Protocols || []).map((p) => (
            <Badge key={p} size="sm" variant="outline" action="info">
              <BadgeText>{p}</BadgeText>
            </Badge>
          ))}
          {resolver.DNSSEC ? <PropertyPill label="DNSSEC" /> : null}
          {resolver.NoLog ? <PropertyPill label="No log" /> : null}
          {resolver.NoFilter ? <PropertyPill label="No filter" /> : null}
        </HStack>
        {resolver.Description ? (
          <Text size="xs" color="$muted500" numberOfLines={2}>
            {resolver.Description}
          </Text>
        ) : null}
      </VStack>
      <VStack alignItems="flex-end" space="xs" flexShrink={0}>
        {live ? (
          <Badge size="sm" variant="outline" action="success">
            <BadgeText sx={{ '@base': { fontFamily: 'monospace' } }}>
              {live.RTTms >= 0 ? `${live.RTTms} ms` : 'live'}
            </BadgeText>
          </Badge>
        ) : null}
        <Badge
          size="sm"
          variant={selected ? 'solid' : 'outline'}
          action={selected ? 'success' : 'muted'}
        >
          <BadgeText>{selected ? 'Selected' : 'Select'}</BadgeText>
        </Badge>
      </VStack>
    </HStack>
  </Pressable>
)

// Multi-select resolver allowlist picker fed by GET /resolvers (the list
// vendored into the image). selected=[] means "automatic": dnscrypt-proxy
// uses every resolver matching the requirement settings. `health` maps
// resolver name -> {Protocol, RTTms} from GET /status while the daemon runs.
export default function ResolverPicker({ resolvers, selected, onChange, health = {} }) {
  const [filterText, setFilterText] = useState('')
  const [flags, setFlags] = useState(EMPTY_FLAGS)

  const toggleFlag = (k) => setFlags({ ...flags, [k]: !flags[k] })
  const clearFilters = () => {
    setFilterText('')
    setFlags(EMPTY_FLAGS)
  }
  const hasFilters =
    filterText.trim() !== '' || Object.values(flags).some(Boolean)

  const toggleName = (name) => {
    if (selected.includes(name)) {
      onChange(selected.filter((n) => n !== name))
    } else {
      onChange([...selected, name])
    }
  }

  const filtered = useMemo(() => {
    const q = filterText.trim().toLowerCase()
    return (resolvers || []).filter((r) => {
      const protos = r.Protocols || []
      if (flags.dnscrypt && !protos.includes('DNSCrypt')) return false
      if (flags.doh && !protos.includes('DoH')) return false
      if (flags.dnssec && !r.DNSSEC) return false
      if (flags.nolog && !r.NoLog) return false
      if (flags.nofilter && !r.NoFilter) return false
      if (flags.selected && !selected.includes(r.Name)) return false
      if (flags.live && !health[r.Name]) return false
      if (
        q &&
        !r.Name.toLowerCase().includes(q) &&
        !(r.Description || '').toLowerCase().includes(q)
      )
        return false
      return true
    })
  }, [resolvers, filterText, flags, selected, health])

  const shown = filtered.slice(0, MAX_SHOWN)

  if (!resolvers || !resolvers.length) {
    return (
      <Card>
        <SectionHeader title="Resolvers" />
        <EmptyState
          title="Resolver list unavailable"
          description="The public-resolvers list vendored into the plugin image could not be loaded. Rebuild or reinstall the plugin container to restore it."
        />
      </Card>
    )
  }

  return (
    <Card>
      <SectionHeader
        title="Resolvers"
        count={selected.length}
        right={
          selected.length ? (
            <Button size="xs" variant="outline" action="secondary" onPress={() => onChange([])}>
              <ButtonText>Clear selection</ButtonText>
            </Button>
          ) : (
            <Badge size="md" variant="outline" action="success" borderRadius="$full">
              <BadgeText>Automatic</BadgeText>
            </Badge>
          )
        }
      />
      <VStack space="md">
        <Text size="xs" color="$muted500">
          {selected.length
            ? `Only the ${selected.length} selected resolver${
                selected.length === 1 ? '' : 's'
              } will be used. They must still satisfy the requirements above.`
            : 'Automatic — no resolvers pinned. dnscrypt-proxy uses every eligible resolver that matches the requirements above and prefers the fastest live ones.'}
        </Text>

        <TextField
          value={filterText}
          onChangeText={setFilterText}
          placeholder="Filter by name or description…"
        />

        <HStack space="sm" flexWrap="wrap" gap="$2" alignItems="center">
          <Text size="xs" color="$muted500">
            Show only:
          </Text>
          <FilterChip label="DNSCrypt" active={flags.dnscrypt} onPress={() => toggleFlag('dnscrypt')} />
          <FilterChip label="DoH" active={flags.doh} onPress={() => toggleFlag('doh')} />
          <FilterChip label="DNSSEC" active={flags.dnssec} onPress={() => toggleFlag('dnssec')} />
          <FilterChip label="No log" active={flags.nolog} onPress={() => toggleFlag('nolog')} />
          <FilterChip label="No filter" active={flags.nofilter} onPress={() => toggleFlag('nofilter')} />
          <FilterChip label="Selected" active={flags.selected} onPress={() => toggleFlag('selected')} />
          <FilterChip label="Live" active={flags.live} onPress={() => toggleFlag('live')} />
        </HStack>

        {shown.length ? (
          <VStack>
            {shown.map((r) => (
              <ResolverRow
                key={r.Name}
                resolver={r}
                selected={selected.includes(r.Name)}
                live={health[r.Name]}
                onPress={() => toggleName(r.Name)}
              />
            ))}
          </VStack>
        ) : (
          <EmptyState
            icon={SearchIcon}
            title="No resolvers match"
            description="Try a different search or remove some filters."
          >
            {hasFilters ? (
              <Button size="xs" variant="outline" action="secondary" onPress={clearFilters}>
                <ButtonText>Clear filters</ButtonText>
              </Button>
            ) : null}
          </EmptyState>
        )}

        <Text size="xs" color="$muted500">
          {filtered.length} match{filtered.length === 1 ? '' : 'es'}
          {filtered.length > MAX_SHOWN ? `, showing first ${MAX_SHOWN} — refine the filter` : ''}
        </Text>
      </VStack>
    </Card>
  )
}
