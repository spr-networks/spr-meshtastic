import React, { useCallback, useEffect, useState } from 'react'
import {
  api,
  useAlert,
  timeAgo,
  Page,
  ListHeader,
  Card,
  SectionHeader,
  StatTile,
  KeyVal,
  StatusDot,
  TextField,
  Loading,
  EmptyState,
  Button,
  ButtonText,
  Badge,
  BadgeText,
  Heading,
  Text,
  HStack,
  VStack,
  Box,
  GlobeIcon,
  MessageCircleIcon,
  HelpCircleIcon
} from '@spr-networks/plugin-ui'

const PLUGIN_BASE = `/plugins/${api.pluginURI() || 'spr-meshtastic'}`

// ---- helpers ----

const MAX_MESSAGE_BYTES = 200 // backend limit (messages.go)
const ONLINE_WINDOW_MS = 2 * 60 * 60 * 1000 // heard within 2h = online (matches /topology)

const heard = (lastHeard) => {
  if (!lastHeard) return '—'
  return timeAgo(new Date(lastHeard * 1000).toISOString()) || '—'
}

const isOnline = (lastHeard) =>
  !!lastHeard && Date.now() - lastHeard * 1000 <= ONLINE_WINDOW_MS

const byteLength = (s) => new TextEncoder().encode(s).length

const isPrivateIPv4 = (s) => {
  const m = s.match(/^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/)
  if (!m) return false
  const o = m.slice(1).map(Number)
  if (o.some((x) => x > 255)) return false
  return (
    o[0] === 10 ||
    (o[0] === 172 && o[1] >= 16 && o[1] <= 31) ||
    (o[0] === 192 && o[1] === 168)
  )
}

const isSerialDevice = (s) => /^\/dev\/tty[A-Za-z0-9]+$/.test(s)

const isValidDest = (s) => /^(![0-9a-fA-F]{8}|[0-9]{1,10})$/.test(s)

// SNR quality wording; thresholds follow Meshtastic's usable-link guidance.
const snrQuality = (snr) => {
  if (snr >= -7) return { label: 'Good', action: 'success' }
  if (snr >= -15) return { label: 'Fair', action: 'warning' }
  return { label: 'Poor', action: 'error' }
}

// ---- small presentational pieces ----

const Pill = ({ action = 'muted', children }) => (
  <Badge size="sm" action={action} variant="outline" borderRadius="$full">
    <BadgeText>{children}</BadgeText>
  </Badge>
)

const BatteryPill = ({ level }) => {
  if (level === null || level === undefined)
    return (
      <Text size="sm" color="$muted500">
        —
      </Text>
    )
  const pct = Math.min(level, 100)
  const action = pct <= 10 ? 'error' : pct <= 25 ? 'warning' : 'success'
  return <Pill action={action}>{pct}%</Pill>
}

// Tile: StatTile styling but with arbitrary children (e.g. a semantic pill).
const Tile = ({ label, children }) => (
  <VStack
    space="xs"
    py="$3"
    px="$4"
    borderRadius="$xl"
    borderWidth={1}
    borderColor="$muted100"
    minWidth={148}
    minHeight={74}
    flexGrow={1}
    flexBasis={148}
    justifyContent="center"
    bg="$backgroundContentLight"
    sx={{
      _dark: { bg: '$backgroundContentDark', borderColor: '$borderColorCardDark' }
    }}
  >
    <Text
      size="2xs"
      color="$muted500"
      fontWeight="$medium"
      sx={{ '@base': { letterSpacing: 0.6, textTransform: 'uppercase' } }}
    >
      {label}
    </Text>
    <Box alignItems="flex-start">{children}</Box>
  </VStack>
)

const ColLabel = ({ children, ...props }) => (
  <Text
    size="2xs"
    color="$muted500"
    fontWeight="$medium"
    sx={{ '@base': { letterSpacing: 0.6, textTransform: 'uppercase' } }}
    {...props}
  >
    {children}
  </Text>
)

const SegButton = ({ active, label, onPress }) => (
  <Button
    size="xs"
    variant={active ? 'solid' : 'outline'}
    action={active ? 'primary' : 'secondary'}
    onPress={onPress}
    flex={1}
  >
    <ButtonText>{label}</ButtonText>
  </Button>
)

// Connection form, shared by the first-run card and the settings card.
const ConnectionForm = ({
  mode,
  setMode,
  host,
  setHost,
  serialDevice,
  setSerialDevice,
  hostError,
  serialError,
  dirty,
  saving,
  onSave
}) => (
  <VStack space="md">
    <HStack gap="$2" maxWidth={360}>
      <SegButton
        active={mode === 'tcp'}
        label="Network (TCP)"
        onPress={() => setMode('tcp')}
      />
      <SegButton
        active={mode === 'serial'}
        label="USB serial"
        onPress={() => setMode('serial')}
      />
    </HStack>
    {mode === 'tcp' ? (
      <TextField
        label="Node LAN IP"
        value={host}
        onChangeText={setHost}
        placeholder="192.168.2.150"
        helper="Meshtastic node with WiFi/Ethernet on your LAN, TCP port 4403. Private (RFC1918) IPv4 only."
        error={hostError}
      />
    ) : (
      <TextField
        label="Serial device"
        value={serialDevice}
        onChangeText={setSerialDevice}
        placeholder="/dev/ttyUSB0"
        helper="Requires passing the device into the container — uncomment the devices/group_add blocks in docker-compose.yml (see README)."
        error={serialError}
      />
    )}
    <HStack alignItems="center" gap="$3" flexWrap="wrap">
      <Button size="sm" onPress={onSave} isDisabled={!dirty || saving}>
        <ButtonText>{saving ? 'Saving…' : 'Save settings'}</ButtonText>
      </Button>
      <Text size="xs" color="$muted500">
        Saving reconnects to the node and refreshes mesh data.
      </Text>
    </HStack>
  </VStack>
)

// ---- main ----

export default function Plugin() {
  const alert = useAlert()
  const [status, setStatus] = useState(null)
  const [nodes, setNodes] = useState([])
  const [messages, setMessages] = useState([])
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [loadError, setLoadError] = useState(null)

  // connection settings form
  const [mode, setMode] = useState('tcp')
  const [host, setHost] = useState('')
  const [serialDevice, setSerialDevice] = useState('/dev/ttyUSB0')
  const [savedCfg, setSavedCfg] = useState(null)
  const [saving, setSaving] = useState(false)

  // composer
  const [msgText, setMsgText] = useState('')
  const [msgTo, setMsgTo] = useState('')
  const [msgChannel, setMsgChannel] = useState('0')
  const [sending, setSending] = useState(false)

  const refresh = useCallback((force = false) => {
    const q = force ? '?refresh=1' : ''
    setRefreshing(true)
    const pStatus = api.get(`${PLUGIN_BASE}/status${q}`).then((s) => {
      setStatus(s)
      setLoadError(null)
    })
    const pMsgs = api
      .get(`${PLUGIN_BASE}/messages`)
      .then(setMessages)
      .catch(() => {})
    const pNodes = api
      .get(`${PLUGIN_BASE}/nodes${q}`)
      .then(setNodes)
      .catch(() => setNodes([]))
    return Promise.all([pStatus, pMsgs, pNodes])
      .catch((err) => setLoadError(err?.message || 'Backend unreachable'))
      .finally(() => {
        setLoading(false)
        setRefreshing(false)
      })
  }, [])

  useEffect(() => {
    api
      .get(`${PLUGIN_BASE}/config`)
      .then((c) => {
        const m = c.ConnectionMode || 'tcp'
        const h = c.Host || ''
        const d = c.SerialDevice || '/dev/ttyUSB0'
        setMode(m)
        setHost(h)
        setSerialDevice(d)
        setSavedCfg({ mode: m, host: h, serialDevice: d })
      })
      .catch(() => {})
    refresh()
  }, [refresh])

  // -- connection settings state --
  const hostError =
    mode === 'tcp' && host.trim() && !isPrivateIPv4(host.trim())
      ? 'Enter a private IPv4 address, e.g. 192.168.2.150'
      : null
  const serialError =
    mode === 'serial' && serialDevice.trim() && !isSerialDevice(serialDevice.trim())
      ? 'Must look like /dev/ttyUSB0 or /dev/ttyACM0'
      : null
  const formComplete =
    mode === 'tcp'
      ? !!host.trim() && !hostError
      : !!serialDevice.trim() && !serialError
  const dirty =
    !savedCfg ||
    savedCfg.mode !== mode ||
    (mode === 'tcp'
      ? savedCfg.host !== host.trim()
      : savedCfg.serialDevice !== serialDevice.trim())

  const saveConfig = () => {
    if (!formComplete) {
      alert.error(hostError || serialError || 'Fill in the connection first')
      return
    }
    const cfg = {
      ConnectionMode: mode,
      Host: mode === 'tcp' ? host.trim() : '',
      SerialDevice: mode === 'serial' ? serialDevice.trim() : ''
    }
    setSaving(true)
    api
      .put(`${PLUGIN_BASE}/config`, cfg)
      .then(() => {
        setSavedCfg({ mode, host: host.trim(), serialDevice: serialDevice.trim() })
        alert.success('Settings saved — connecting to the node')
        refresh(true)
      })
      .catch((err) => alert.error('Failed to save settings', err))
      .finally(() => setSaving(false))
  }

  // -- composer state --
  const msgBytes = byteLength(msgText)
  const msgTooLong = msgBytes > MAX_MESSAGE_BYTES
  const destError =
    msgTo.trim() && !isValidDest(msgTo.trim())
      ? 'Use a node id like !a1b2c3d4 or a node number'
      : null
  const channelNum = msgChannel.trim() === '' ? 0 : Number(msgChannel.trim())
  const channelError =
    !Number.isInteger(channelNum) || channelNum < 0 || channelNum > 7
      ? 'Channel index must be 0–7'
      : null
  const canSend =
    msgText.length > 0 && !msgTooLong && !destError && !channelError && !sending

  const sendMessage = () => {
    setSending(true)
    api
      .post(`${PLUGIN_BASE}/message`, {
        Text: msgText,
        To: msgTo.trim(),
        Channel: channelNum
      })
      .then(() => {
        alert.success('Message sent to mesh')
        setMsgText('')
        return api.get(`${PLUGIN_BASE}/messages`).then(setMessages)
      })
      .catch((err) => alert.error('Failed to send', err))
      .finally(() => setSending(false))
  }

  const copyText = (text) => {
    navigator.clipboard
      .writeText(text)
      .then(() => alert.success('Copied'))
      .catch(() => alert.error('Copy failed'))
  }

  if (loading) {
    return (
      <Page>
        <Loading text="Talking to the Meshtastic node…" />
      </Page>
    )
  }

  const configured = !!status?.Configured
  const connected = !!status?.Connected
  const connDesc =
    status?.ConnectionMode === 'serial'
      ? status?.SerialDevice || 'USB serial'
      : status?.Host
      ? `${status.Host}:4403`
      : 'not set'

  const sortedNodes = [...nodes].sort(
    (a, b) => (b.LastHeard || 0) - (a.LastHeard || 0)
  )
  const onlineCount = sortedNodes.filter((n) => isOnline(n.LastHeard)).length

  const header = (
    <ListHeader
      title="Meshtastic"
      description="LoRa mesh gateway — bridge a Meshtastic node to your router"
      mark="mt"
      status={connected ? 'Connected' : configured ? 'Unreachable' : 'Not configured'}
      statusAction={connected ? 'success' : configured ? 'warning' : 'muted'}
    >
      <Button
        size="sm"
        variant="outline"
        action="secondary"
        onPress={() => refresh(true)}
        isDisabled={refreshing}
      >
        <ButtonText>{refreshing ? 'Refreshing…' : 'Refresh'}</ButtonText>
      </Button>
    </ListHeader>
  )

  // Backend unreachable: one error card with retry, nothing else.
  if (!status) {
    return (
      <Page>
        {header}
        <Card>
          <EmptyState
            icon={HelpCircleIcon}
            title="Can't reach the plugin backend"
            description={loadError || 'The backend did not return a status.'}
          >
            <Button size="sm" onPress={() => refresh()} isDisabled={refreshing}>
              <ButtonText>{refreshing ? 'Retrying…' : 'Retry'}</ButtonText>
            </Button>
          </EmptyState>
        </Card>
      </Page>
    )
  }

  // First run: a single guided setup card, no empty widgets.
  if (!configured) {
    return (
      <Page>
        {header}
        <Card>
          <SectionHeader title="Connect your Meshtastic node" />
          <VStack space="md">
            <VStack space="xs">
              <Text size="sm" color="$muted500">
                1. On the node, enable WiFi and the network API (Meshtastic app →
                Radio configuration → Network).
              </Text>
              <Text size="sm" color="$muted500">
                2. Point the plugin at your node's LAN IP below — or plug it into
                USB and pick serial.
              </Text>
              <Text size="sm" color="$muted500">
                3. Save. The gateway connects and your mesh shows up here and in
                SPR's topology view.
              </Text>
            </VStack>
            <ConnectionForm
              mode={mode}
              setMode={setMode}
              host={host}
              setHost={setHost}
              serialDevice={serialDevice}
              setSerialDevice={setSerialDevice}
              hostError={hostError}
              serialError={serialError}
              dirty={dirty && formComplete}
              saving={saving}
              onSave={saveConfig}
            />
          </VStack>
        </Card>
      </Page>
    )
  }

  const lowBattery =
    connected &&
    status.BatteryLevel !== null &&
    status.BatteryLevel !== undefined &&
    Math.min(status.BatteryLevel, 100) <= 25

  return (
    <Page>
      {header}

      {/* Hero: connection state + node identity */}
      <Card>
        <VStack space="lg">
          <HStack space="md" alignItems="center">
            <StatusDot online={connected} warn={!connected} />
            <VStack space="xs" flex={1}>
              <HStack alignItems="center" gap="$2" flexWrap="wrap">
                <Heading
                  size="md"
                  color="$textLight900"
                  sx={{ _dark: { color: '$textDark50' } }}
                >
                  {connected ? status.Owner || 'Meshtastic node' : 'Meshtastic node'}
                </Heading>
                {connected && status.OwnerShort ? (
                  <Pill>{status.OwnerShort}</Pill>
                ) : null}
                {lowBattery ? (
                  <Pill action={status.BatteryLevel <= 10 ? 'error' : 'warning'}>
                    Low battery
                  </Pill>
                ) : null}
              </HStack>
              {connected ? (
                <Text size="xs" color="$muted500" fontFamily="$mono">
                  {[status.NodeID, status.HwModel, status.Firmware && `fw ${status.Firmware}`]
                    .filter(Boolean)
                    .join(' · ') || '—'}
                </Text>
              ) : null}
              <Text size="sm" color="$muted500">
                {connected ? 'Connected' : 'Unreachable'} ·{' '}
                {status.ConnectionMode === 'serial' ? 'USB serial' : 'network'}{' '}
                {connDesc}
              </Text>
            </VStack>
          </HStack>

          {connected ? (
            <VStack space="md">
              <HStack flexWrap="wrap" gap="$2">
                <Tile label="Battery">
                  <BatteryPill level={status.BatteryLevel} />
                </Tile>
                <StatTile label="Mesh nodes" value={`${status.NumNodes || 0}`} />
                <StatTile
                  label="Channel"
                  value={status.ChannelURL ? 'Primary (ch 0)' : '—'}
                />
                <StatTile
                  label="Last refresh"
                  value={
                    status.LastUpdated ? timeAgo(status.LastUpdated) || '—' : '—'
                  }
                />
              </HStack>
              {status.ChannelURL ? (
                <HStack alignItems="center" gap="$2" flexWrap="wrap">
                  <Box flex={1} minWidth={200}>
                    <KeyVal
                      label="Primary channel URL"
                      value={status.ChannelURL}
                      mono
                    />
                  </Box>
                  <Button
                    size="xs"
                    variant="outline"
                    action="secondary"
                    onPress={() => copyText(status.ChannelURL)}
                  >
                    <ButtonText>Copy</ButtonText>
                  </Button>
                </HStack>
              ) : null}
            </VStack>
          ) : (
            <VStack space="xs">
              {status.Error ? (
                <Text size="sm" color="$muted500">
                  {status.Error}
                </Text>
              ) : null}
              <Text size="sm" color="$muted500">
                Check that the node is powered and reachable, then Refresh — or
                update the connection settings below.
              </Text>
            </VStack>
          )}
        </VStack>
      </Card>

      {/* Nodes table */}
      {connected ? (
        <Card>
          <SectionHeader
            title="Nodes in mesh"
            count={sortedNodes.length}
            right={
              sortedNodes.length ? (
                <Text size="xs" color="$muted500">
                  {onlineCount} heard in the last 2h
                </Text>
              ) : null
            }
          />
          {sortedNodes.length === 0 ? (
            <EmptyState
              icon={GlobeIcon}
              title="No mesh nodes heard yet"
              description="Nodes appear here as soon as the radio hears them. Keep the gateway powered and within LoRa range of your mesh."
            />
          ) : (
            <VStack>
              <HStack alignItems="center" gap="$3" pb="$2">
                <Box w={18} />
                <Box flex={1} minWidth={150}>
                  <ColLabel>Node</ColLabel>
                </Box>
                <Box minWidth={130} alignItems="flex-end">
                  <ColLabel>SNR</ColLabel>
                </Box>
                <Box minWidth={90} alignItems="flex-end">
                  <ColLabel>Last heard</ColLabel>
                </Box>
                <Box minWidth={70} alignItems="flex-end">
                  <ColLabel>Battery</ColLabel>
                </Box>
              </HStack>
              {sortedNodes.map((n) => {
                const q =
                  n.SNR === null || n.SNR === undefined ? null : snrQuality(n.SNR)
                return (
                  <HStack
                    key={n.ID}
                    alignItems="center"
                    gap="$3"
                    py="$2.5"
                    borderTopWidth={1}
                    borderColor="$muted100"
                    sx={{ _dark: { borderColor: '$borderColorCardDark' } }}
                  >
                    <Box w={18} alignItems="center">
                      <StatusDot online={isOnline(n.LastHeard)} size={8} />
                    </Box>
                    <VStack flex={1} minWidth={150} space="xs">
                      <HStack alignItems="center" gap="$2" flexWrap="wrap">
                        <Text size="sm" bold>
                          {n.LongName || n.ID}
                        </Text>
                        {n.IsSelf ? <Pill action="info">gateway</Pill> : null}
                      </HStack>
                      <Text size="xs" color="$muted500" fontFamily="$mono">
                        {n.ID}
                        {n.ShortName ? ` · ${n.ShortName}` : ''}
                        {n.HopsAway ? ` · ${n.HopsAway} hop${n.HopsAway > 1 ? 's' : ''}` : ''}
                      </Text>
                    </VStack>
                    <HStack
                      minWidth={130}
                      justifyContent="flex-end"
                      alignItems="center"
                      gap="$2"
                    >
                      {q ? (
                        <>
                          <Text size="sm" fontFamily="$mono">
                            {n.SNR} dB
                          </Text>
                          <Pill action={q.action}>{q.label}</Pill>
                        </>
                      ) : (
                        <Text size="sm" color="$muted500">
                          —
                        </Text>
                      )}
                    </HStack>
                    <Box minWidth={90} alignItems="flex-end">
                      <Text size="sm" color="$muted500">
                        {heard(n.LastHeard)}
                      </Text>
                    </Box>
                    <Box minWidth={70} alignItems="flex-end">
                      <BatteryPill level={n.BatteryLevel} />
                    </Box>
                  </HStack>
                )
              })}
            </VStack>
          )}
        </Card>
      ) : null}

      {/* Composer */}
      <Card>
        <SectionHeader title="Send a message" />
        <VStack space="md">
          <TextField
            label="Message"
            value={msgText}
            onChangeText={setMsgText}
            placeholder="Text to send over the mesh…"
            helper={`${msgBytes}/${MAX_MESSAGE_BYTES} bytes`}
            error={
              msgTooLong
                ? `Too long — ${msgBytes}/${MAX_MESSAGE_BYTES} bytes (LoRa payload limit)`
                : null
            }
          />
          <HStack gap="$4" flexWrap="wrap">
            <Box flex={2} minWidth={180}>
              <TextField
                label="Destination"
                value={msgTo}
                onChangeText={setMsgTo}
                placeholder="!a1b2c3d4"
                helper="Leave empty to broadcast to everyone."
                error={destError}
              />
            </Box>
            <Box flex={1} minWidth={110}>
              <TextField
                label="Channel"
                value={msgChannel}
                onChangeText={setMsgChannel}
                placeholder="0"
                helper="Index 0–7 (0 = primary)."
                error={channelError}
              />
            </Box>
          </HStack>
          <Box alignItems="flex-start">
            <Button size="sm" onPress={sendMessage} isDisabled={!canSend}>
              <ButtonText>{sending ? 'Sending…' : 'Send to mesh'}</ButtonText>
            </Button>
          </Box>
        </VStack>
      </Card>

      {/* Message feed */}
      <Card>
        <SectionHeader title="Message log" count={messages.length} />
        {messages.length === 0 ? (
          <EmptyState
            icon={MessageCircleIcon}
            title="No messages yet"
            description="Messages you send from here show up in this log. Incoming mesh traffic isn't captured — this log is send-only (see README)."
          />
        ) : (
          <VStack>
            <Text size="xs" color="$muted500" pb="$2">
              Sent from this plugin only — incoming mesh traffic isn't captured
              (see README).
            </Text>
            {messages.slice(0, 20).map((m, i) => {
              const failed = m.Status !== 'sent'
              return (
                <HStack
                  key={`${m.Time}-${i}`}
                  alignItems="flex-start"
                  justifyContent="space-between"
                  gap="$3"
                  py="$2.5"
                  borderTopWidth={1}
                  borderColor="$muted100"
                  sx={{ _dark: { borderColor: '$borderColorCardDark' } }}
                >
                  <VStack flex={1} minWidth={160} space="xs">
                    <HStack alignItems="center" gap="$2" flexWrap="wrap">
                      <Text size="sm" bold>
                        You → {m.To || 'broadcast'}
                      </Text>
                      <Pill>ch {m.Channel}</Pill>
                      <Text size="xs" color="$muted500">
                        {timeAgo(m.Time) || '—'}
                      </Text>
                    </HStack>
                    <Text size="sm">{m.Text}</Text>
                    {failed ? (
                      <Text size="xs" color="$muted500">
                        {m.Status}
                      </Text>
                    ) : null}
                  </VStack>
                  <Pill action={failed ? 'error' : 'success'}>
                    {failed ? 'failed' : 'sent'}
                  </Pill>
                </HStack>
              )
            })}
          </VStack>
        )}
      </Card>

      {/* Settings */}
      <Card>
        <SectionHeader title="Connection settings" />
        <ConnectionForm
          mode={mode}
          setMode={setMode}
          host={host}
          setHost={setHost}
          serialDevice={serialDevice}
          setSerialDevice={setSerialDevice}
          hostError={hostError}
          serialError={serialError}
          dirty={dirty && formComplete}
          saving={saving}
          onSave={saveConfig}
        />
      </Card>
    </Page>
  )
}
