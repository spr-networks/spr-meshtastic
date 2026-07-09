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
  Text,
  HStack,
  VStack,
  Box
} from '@spr-networks/plugin-ui'

const PLUGIN_BASE = `/plugins/${api.pluginURI() || 'spr-meshtastic'}`

const heard = (lastHeard) => {
  if (!lastHeard) return '—'
  return timeAgo(new Date(lastHeard * 1000).toISOString()) || '—'
}

const battery = (level) =>
  level === null || level === undefined ? '—' : `${Math.min(level, 100)}%`

const ModeButton = ({ active, label, onPress }) => (
  <Button
    size="xs"
    variant={active ? 'solid' : 'outline'}
    onPress={onPress}
    flex={1}
  >
    <ButtonText>{label}</ButtonText>
  </Button>
)

export default function Plugin() {
  const alert = useAlert()
  const [status, setStatus] = useState(null)
  const [nodes, setNodes] = useState([])
  const [messages, setMessages] = useState([])
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)

  // settings form
  const [mode, setMode] = useState('tcp')
  const [host, setHost] = useState('')
  const [serialDevice, setSerialDevice] = useState('/dev/ttyUSB0')

  // send form
  const [msgText, setMsgText] = useState('')
  const [msgTo, setMsgTo] = useState('')
  const [msgChannel, setMsgChannel] = useState('0')
  const [sending, setSending] = useState(false)

  const refresh = useCallback((force = false) => {
    const q = force ? '?refresh=1' : ''
    setRefreshing(true)
    const pStatus = api.get(`${PLUGIN_BASE}/status${q}`).then(setStatus)
    const pMsgs = api.get(`${PLUGIN_BASE}/messages`).then(setMessages)
    const pNodes = api
      .get(`${PLUGIN_BASE}/nodes${q}`)
      .then(setNodes)
      .catch(() => setNodes([]))
    return Promise.all([pStatus, pMsgs, pNodes])
      .catch((err) => alert.error('Failed to load status', err))
      .finally(() => {
        setLoading(false)
        setRefreshing(false)
      })
  }, [])

  useEffect(() => {
    api
      .get(`${PLUGIN_BASE}/config`)
      .then((c) => {
        setMode(c.ConnectionMode || 'tcp')
        setHost(c.Host || '')
        setSerialDevice(c.SerialDevice || '/dev/ttyUSB0')
      })
      .catch(() => {})
    refresh()
  }, [refresh])

  const saveConfig = () => {
    const cfg = {
      ConnectionMode: mode,
      Host: mode === 'tcp' ? host.trim() : '',
      SerialDevice: mode === 'serial' ? serialDevice.trim() : ''
    }
    api
      .put(`${PLUGIN_BASE}/config`, cfg)
      .then(() => {
        alert.success('Settings saved')
        setLoading(true)
        refresh(true)
      })
      .catch((err) => alert.error('Failed to save settings', err))
  }

  const sendMessage = () => {
    const body = {
      Text: msgText,
      To: msgTo.trim(),
      Channel: parseInt(msgChannel, 10) || 0
    }
    setSending(true)
    api
      .post(`${PLUGIN_BASE}/message`, body)
      .then(() => {
        alert.success('Message sent to mesh')
        setMsgText('')
        return api.get(`${PLUGIN_BASE}/messages`).then(setMessages)
      })
      .catch((err) => alert.error('Failed to send', err))
      .finally(() => setSending(false))
  }

  if (loading) {
    return (
      <Page>
        <Loading text="Talking to the Meshtastic node..." />
      </Page>
    )
  }

  const configured = !!status?.Configured
  const connected = !!status?.Connected
  const connLabel =
    status?.ConnectionMode === 'serial'
      ? status?.SerialDevice || 'serial'
      : status?.Host
      ? `${status.Host}:4403`
      : 'not set'

  return (
    <Page>
      <ListHeader
        title="Meshtastic"
        description="LoRa mesh gateway — bridge a Meshtastic node to your router"
      >
        <Button size="sm" onPress={() => refresh(true)} isDisabled={refreshing}>
          <ButtonText>{refreshing ? 'Refreshing...' : 'Refresh'}</ButtonText>
        </Button>
      </ListHeader>

      <Card>
        <SectionHeader
          title="Node"
          right={<StatusDot online={connected} warn={configured && !connected} />}
        />
        {!configured ? (
          <Text size="sm" color="$muted500">
            Set the connection to your Meshtastic node below to get started.
          </Text>
        ) : (
          <VStack space="md">
            <HStack flexWrap="wrap" gap="$2">
              <StatTile
                label="Node"
                value={
                  status.Owner
                    ? `${status.Owner}${status.OwnerShort ? ` (${status.OwnerShort})` : ''}`
                    : '—'
                }
              />
              <StatTile label="Connection" value={connLabel} mono />
              <StatTile label="Battery" value={battery(status.BatteryLevel)} />
              <StatTile label="Mesh nodes" value={`${status.NumNodes || 0}`} />
              <StatTile label="Hardware" value={status.HwModel || '—'} />
              <StatTile label="Firmware" value={status.Firmware || '—'} mono />
            </HStack>
            {connected && status.ChannelURL ? (
              <KeyVal label="Primary channel URL" value={status.ChannelURL} mono />
            ) : null}
            {!connected && status.Error ? (
              <Text size="sm" color="$muted500">
                {status.Error}
              </Text>
            ) : null}
          </VStack>
        )}
      </Card>

      {configured && connected ? (
        <Card>
          <SectionHeader title="Nodes in mesh" count={nodes.length} />
          {nodes.length === 0 ? (
            <Text size="sm" color="$muted500">
              No nodes heard yet.
            </Text>
          ) : (
            <VStack space="sm">
              {nodes.map((n) => (
                <HStack
                  key={n.ID}
                  alignItems="center"
                  justifyContent="space-between"
                  flexWrap="wrap"
                  gap="$2"
                  py="$1"
                >
                  <VStack flex={2} minWidth={140}>
                    <HStack alignItems="center" gap="$2">
                      <Text size="sm" bold>
                        {n.LongName || n.ID}
                      </Text>
                      {n.IsSelf ? (
                        <Badge size="sm" action="info" variant="outline">
                          <BadgeText>this node</BadgeText>
                        </Badge>
                      ) : null}
                    </HStack>
                    <Text size="xs" color="$muted500" fontFamily="$mono">
                      {n.ShortName ? `${n.ShortName} · ` : ''}
                      {n.ID}
                    </Text>
                  </VStack>
                  <HStack flex={3} gap="$4" flexWrap="wrap">
                    <KeyVal
                      label="SNR"
                      value={n.SNR === null || n.SNR === undefined ? '—' : `${n.SNR} dB`}
                      mono
                    />
                    <KeyVal label="Last heard" value={heard(n.LastHeard)} />
                    <KeyVal label="Battery" value={battery(n.BatteryLevel)} mono />
                  </HStack>
                </HStack>
              ))}
            </VStack>
          )}
        </Card>
      ) : null}

      {configured ? (
        <Card>
          <SectionHeader title="Send message" />
          <VStack space="md">
            <TextField
              label="Message"
              value={msgText}
              onChangeText={setMsgText}
              placeholder="Text to broadcast on the mesh..."
              helper="Up to 200 bytes, sent with the meshtastic CLI"
            />
            <HStack gap="$4" flexWrap="wrap">
              <Box flex={2} minWidth={160}>
                <TextField
                  label="Destination (optional)"
                  value={msgTo}
                  onChangeText={setMsgTo}
                  placeholder="!abcd1234 — empty = broadcast"
                />
              </Box>
              <Box flex={1} minWidth={100}>
                <TextField
                  label="Channel"
                  value={msgChannel}
                  onChangeText={setMsgChannel}
                  placeholder="0"
                  helper="Index 0-7"
                />
              </Box>
            </HStack>
            <Button
              size="sm"
              onPress={sendMessage}
              isDisabled={sending || !msgText.length}
            >
              <ButtonText>{sending ? 'Sending...' : 'Send to mesh'}</ButtonText>
            </Button>
          </VStack>
        </Card>
      ) : null}

      {configured && messages.length ? (
        <Card>
          <SectionHeader title="Message log" count={messages.length} />
          <VStack space="sm">
            {messages.slice(0, 20).map((m, i) => (
              <HStack
                key={`${m.Time}-${i}`}
                justifyContent="space-between"
                flexWrap="wrap"
                gap="$2"
              >
                <VStack flex={1} minWidth={160}>
                  <Text size="sm">{m.Text}</Text>
                  <Text size="xs" color="$muted500">
                    to {m.To || 'broadcast'} · ch {m.Channel} ·{' '}
                    {timeAgo(m.Time) || m.Time}
                  </Text>
                </VStack>
                <Badge
                  size="sm"
                  action={m.Status === 'sent' ? 'success' : 'error'}
                  variant="outline"
                >
                  <BadgeText>{m.Status === 'sent' ? 'sent' : 'failed'}</BadgeText>
                </Badge>
              </HStack>
            ))}
            <Text size="xs" color="$muted500">
              Messages sent through this plugin. Live RX capture is not part of
              the MVP — see the README.
            </Text>
          </VStack>
        </Card>
      ) : null}

      <Card>
        <SectionHeader title="Connection settings" />
        <VStack space="md">
          <HStack gap="$2">
            <ModeButton
              active={mode === 'tcp'}
              label="Network (TCP)"
              onPress={() => setMode('tcp')}
            />
            <ModeButton
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
              helper="Meshtastic node with WiFi/Ethernet on your LAN (TCP port 4403). Private (RFC1918) IPv4 only."
            />
          ) : (
            <TextField
              label="Serial device"
              value={serialDevice}
              onChangeText={setSerialDevice}
              placeholder="/dev/ttyUSB0"
              helper="Requires passing the device into the container — uncomment the devices/group_add blocks in docker-compose.yml (see README)."
            />
          )}
          <Button size="sm" onPress={saveConfig}>
            <ButtonText>Save settings</ButtonText>
          </Button>
        </VStack>
      </Card>

      {!configured && !status ? (
        <EmptyState
          title="No status"
          description="The backend did not return a status."
        >
          <Button size="sm" onPress={() => refresh()}>
            <ButtonText>Retry</ButtonText>
          </Button>
        </EmptyState>
      ) : null}
    </Page>
  )
}
