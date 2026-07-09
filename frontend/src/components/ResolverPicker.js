import React, { useMemo, useState } from 'react'
import {
  Badge,
  BadgeText,
  Button,
  ButtonText,
  Card,
  HStack,
  Pressable,
  SectionHeader,
  Text,
  TextField,
  Toggle,
  VStack
} from '@spr-networks/plugin-ui'

const MAX_SHOWN = 60

const FlagBadge = ({ label, on }) => (
  <Badge size="sm" variant="outline" action={on ? 'success' : 'muted'}>
    <BadgeText>{label}</BadgeText>
  </Badge>
)

const ResolverRow = ({ resolver, selected, onPress }) => (
  <Pressable onPress={onPress}>
    <HStack
      space="md"
      alignItems="center"
      justifyContent="space-between"
      py="$2"
      px="$2"
      borderBottomWidth={1}
      borderColor="$borderColorCardLight"
      sx={{
        _dark: { borderColor: '$borderColorCardDark' },
        ...(selected ? { bg: '$primary100', _dark: { bg: '$primary900', borderColor: '$borderColorCardDark' } } : {})
      }}
    >
      <VStack flex={1} space="xs">
        <HStack space="sm" alignItems="center" flexWrap="wrap">
          <Text size="sm" bold>
            {resolver.Name}
          </Text>
          {(resolver.Protocols || []).map((p) => (
            <Badge key={p} size="sm" variant="solid" action="info">
              <BadgeText>{p}</BadgeText>
            </Badge>
          ))}
          <FlagBadge label="DNSSEC" on={resolver.DNSSEC} />
          <FlagBadge label="No log" on={resolver.NoLog} />
          <FlagBadge label="No filter" on={resolver.NoFilter} />
        </HStack>
        <Text size="xs" color="$muted500" numberOfLines={2}>
          {resolver.Description}
        </Text>
      </VStack>
      <Badge size="sm" variant={selected ? 'solid' : 'outline'} action={selected ? 'success' : 'muted'}>
        <BadgeText>{selected ? 'Selected' : 'Select'}</BadgeText>
      </Badge>
    </HStack>
  </Pressable>
)

const FilterToggle = ({ label, value, onPress }) => (
  <HStack space="sm" alignItems="center">
    <Toggle value={value} onPress={onPress} />
    <Text size="xs" color="$muted500">
      {label}
    </Text>
  </HStack>
)

// Multi-select resolver picker fed by GET /resolvers (the list vendored into
// the image). selected=[] means "all resolvers matching the filters".
export default function ResolverPicker({ resolvers, selected, onChange }) {
  const [filterText, setFilterText] = useState('')
  const [flags, setFlags] = useState({
    dnscrypt: true,
    doh: true,
    dnssec: false,
    nolog: false,
    nofilter: false
  })

  const toggleFlag = (k) => setFlags({ ...flags, [k]: !flags[k] })

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
      if (!flags.dnscrypt && protos.includes('DNSCrypt') && !protos.includes('DoH')) return false
      if (!flags.doh && protos.includes('DoH') && !protos.includes('DNSCrypt')) return false
      if (flags.dnssec && !r.DNSSEC) return false
      if (flags.nolog && !r.NoLog) return false
      if (flags.nofilter && !r.NoFilter) return false
      if (
        q &&
        !r.Name.toLowerCase().includes(q) &&
        !(r.Description || '').toLowerCase().includes(q)
      )
        return false
      return true
    })
  }, [resolvers, filterText, flags])

  const shown = filtered.slice(0, MAX_SHOWN)

  return (
    <Card>
      <SectionHeader
        title="Resolvers"
        count={selected.length}
        right={
          selected.length ? (
            <Button size="xs" variant="outline" onPress={() => onChange([])}>
              <ButtonText>Clear selection</ButtonText>
            </Button>
          ) : null
        }
      />
      <VStack space="md">
        <Text size="xs" color="$muted500">
          Pick specific resolvers to allowlist. With nothing selected,
          dnscrypt-proxy uses every resolver from the list that matches the
          requirement settings above.
        </Text>

        <TextField
          label="Filter"
          value={filterText}
          onChangeText={setFilterText}
          placeholder="Search by name or description..."
        />

        <HStack space="lg" flexWrap="wrap">
          <FilterToggle label="DNSCrypt" value={flags.dnscrypt} onPress={() => toggleFlag('dnscrypt')} />
          <FilterToggle label="DoH" value={flags.doh} onPress={() => toggleFlag('doh')} />
          <FilterToggle label="DNSSEC only" value={flags.dnssec} onPress={() => toggleFlag('dnssec')} />
          <FilterToggle label="No-log only" value={flags.nolog} onPress={() => toggleFlag('nolog')} />
          <FilterToggle label="No-filter only" value={flags.nofilter} onPress={() => toggleFlag('nofilter')} />
        </HStack>

        <VStack>
          {shown.map((r) => (
            <ResolverRow
              key={r.Name}
              resolver={r}
              selected={selected.includes(r.Name)}
              onPress={() => toggleName(r.Name)}
            />
          ))}
        </VStack>

        <Text size="xs" color="$muted500">
          {filtered.length} match{filtered.length === 1 ? '' : 'es'}
          {filtered.length > MAX_SHOWN ? `, showing first ${MAX_SHOWN} — refine the filter` : ''}
        </Text>
      </VStack>
    </Card>
  )
}
