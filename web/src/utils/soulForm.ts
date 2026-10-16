import type { Soul } from '../api/client'

export type SoulType = Soul['type']

export type SoulFormData = {
  name: string
  type: SoulType
  target: string
  enabled: boolean
  weight: number
  timeout: number
  tags: string[]
  httpMethod: string
  httpValidStatus: string
  tcpSend: string
  tcpExpectRegex: string
  udpSendHex: string
  udpExpectContains: string
  dnsRecordType: string
  dnsExpected: string
  icmpCount: number
  icmpInterval: number
  icmpMaxLossPercent: number
  smtpStartTLS: boolean
  smtpBannerContains: string
  imapTLS: boolean
  imapMailbox: string
  imapUsername: string
  imapPassword: string
  grpcService: string
  websocketPingCheck: boolean
  websocketSend: string
  websocketExpectContains: string
  tlsExpiryWarnDays: number
  tlsExpiryCriticalDays: number
}

export const defaultSoulFormData: SoulFormData = {
  name: '',
  type: 'http',
  target: '',
  enabled: true,
  weight: 60,
  timeout: 10,
  tags: [],
  httpMethod: 'GET',
  httpValidStatus: '200',
  tcpSend: '',
  tcpExpectRegex: '',
  udpSendHex: '',
  udpExpectContains: '',
  dnsRecordType: 'A',
  dnsExpected: '',
  icmpCount: 4,
  icmpInterval: 1,
  icmpMaxLossPercent: 100,
  smtpStartTLS: true,
  smtpBannerContains: '',
  imapTLS: true,
  imapMailbox: '',
  imapUsername: '',
  imapPassword: '',
  grpcService: '',
  websocketPingCheck: true,
  websocketSend: '',
  websocketExpectContains: '',
  tlsExpiryWarnDays: 30,
  tlsExpiryCriticalDays: 7,
}

export const soulTypeOptions: Array<{ value: SoulType; label: string }> = [
  { value: 'http', label: 'HTTP' },
  { value: 'tcp', label: 'TCP' },
  { value: 'udp', label: 'UDP' },
  { value: 'dns', label: 'DNS' },
  { value: 'icmp', label: 'ICMP' },
  { value: 'smtp', label: 'SMTP' },
  { value: 'imap', label: 'IMAP' },
  { value: 'grpc', label: 'gRPC' },
  { value: 'websocket', label: 'WebSocket' },
  { value: 'tls', label: 'TLS' },
]

export const soulTargetHints: Record<SoulType, { label: string; placeholder: string; help: string }> = {
  http: {
    label: 'HTTP URL',
    placeholder: 'https://api.example.com/health',
    help: 'Full URL to request, including http:// or https://.',
  },
  tcp: {
    label: 'TCP Host and Port',
    placeholder: 'api.example.com:443',
    help: 'Host:port pair for a TCP connect check.',
  },
  udp: {
    label: 'UDP Host and Port',
    placeholder: 'dns.example.com:53',
    help: 'Host:port pair for a UDP probe.',
  },
  dns: {
    label: 'DNS Name',
    placeholder: 'example.com',
    help: 'Domain name to resolve.',
  },
  icmp: {
    label: 'ICMP Host or IP',
    placeholder: '1.1.1.1',
    help: 'Hostname or IP address to ping.',
  },
  smtp: {
    label: 'SMTP Host and Port',
    placeholder: 'mail.example.com:587',
    help: 'Mail server host:port for SMTP connectivity.',
  },
  imap: {
    label: 'IMAP Host and Port',
    placeholder: 'mail.example.com:993',
    help: 'Mail server host:port; 993 for implicit TLS, 143 for plain IMAP.',
  },
  grpc: {
    label: 'gRPC Host and Port',
    placeholder: 'grpc.example.com:443',
    help: 'gRPC endpoint host:port.',
  },
  websocket: {
    label: 'WebSocket URL',
    placeholder: 'wss://stream.example.com/health',
    help: 'WebSocket URL, usually ws:// or wss://.',
  },
  tls: {
    label: 'TLS Host and Port',
    placeholder: 'api.example.com:443',
    help: 'Host:port pair whose certificate should be checked.',
  },
}

const parseNumber = (value: number, fallback: number) => Number.isFinite(value) ? value : fallback

const parseIntervalSeconds = (value = '') => {
  const legacy = Number(value)
  if (Number.isFinite(legacy) && legacy > 0) return legacy

  const units: Record<string, number> = { ns: 1e-9, us: 1e-6, 'µs': 1e-6, 'μs': 1e-6, ms: 1e-3, s: 1, m: 60, h: 3600 }
  let consumed = ''
  let seconds = 0
  for (const match of value.matchAll(/(\d+(?:\.\d*)?|\.\d+)(ns|us|µs|μs|ms|s|m|h)/g)) {
    consumed += match[0]
    seconds += Number(match[1]) * units[match[2]]
  }
  return consumed === value && Number.isFinite(seconds) && seconds > 0 ? seconds : 1
}

const parseNumberList = (value: string, fallback: number[]) => {
  const parsed = value
    .split(/[,\s]+/)
    .map((part) => Number(part.trim()))
    .filter((part) => Number.isInteger(part) && part > 0)
  return parsed.length > 0 ? parsed : fallback
}

const parseStringList = (value: string) =>
  value
    .split(/[,\n]+/)
    .map((part) => part.trim())
    .filter(Boolean)

export const nextSoulFormDataForType = (current: SoulFormData, type: SoulType): SoulFormData => ({
  ...defaultSoulFormData,
  name: current.name,
  type,
  enabled: current.enabled,
  weight: current.weight,
  timeout: current.timeout,
  tags: current.tags,
})

export function soulFormDataFromSoul(soul: Soul): SoulFormData {
  const http = soul.http ?? soul.http_config
  const tcp = soul.tcp ?? soul.tcp_config
  const dns = soul.dns ?? soul.dns_config

  return {
    ...defaultSoulFormData,
    name: soul.name || '',
    type: soul.type || 'http',
    target: soul.target || '',
    enabled: soul.enabled ?? true,
    weight: typeof soul.weight === 'number' ? soul.weight : 60,
    timeout: typeof soul.timeout === 'number' ? soul.timeout : 10,
    tags: soul.tags || [],
    httpMethod: http?.method || 'GET',
    httpValidStatus: http?.valid_status?.join(', ') || '200',
    tcpSend: tcp?.send || '',
    tcpExpectRegex: tcp?.expect_regex || '',
    udpSendHex: soul.udp?.send_hex || '',
    udpExpectContains: soul.udp?.expect_contains || '',
    dnsRecordType: dns?.record_type || 'A',
    dnsExpected: dns?.expected?.join(', ') || '',
    icmpCount: soul.icmp?.count ?? 4,
    icmpInterval: parseIntervalSeconds(soul.icmp?.interval),
    icmpMaxLossPercent: soul.icmp?.max_loss_percent ?? 100,
    smtpStartTLS: soul.smtp?.starttls ?? true,
    smtpBannerContains: soul.smtp?.banner_contains || '',
    imapTLS: soul.imap?.tls ?? true,
    imapMailbox: soul.imap?.check_mailbox || '',
    imapUsername: soul.imap?.auth?.username || '',
    imapPassword: soul.imap?.auth?.password || '',
    grpcService: soul.grpc?.service || '',
    websocketPingCheck: soul.websocket?.ping_check ?? true,
    websocketSend: soul.websocket?.send || '',
    websocketExpectContains: soul.websocket?.expect_contains || '',
    tlsExpiryWarnDays: soul.tls?.expiry_warn_days ?? 30,
    tlsExpiryCriticalDays: soul.tls?.expiry_critical_days ?? 7,
  }
}

export function buildSoulPayload(
  formData: SoulFormData,
  workspaceID = 'default'
): Omit<Soul, 'id' | 'created_at' | 'updated_at'> {
  const payload: Omit<Soul, 'id' | 'created_at' | 'updated_at'> = {
    name: formData.name,
    type: formData.type,
    target: formData.target,
    enabled: formData.enabled,
    weight: parseNumber(formData.weight, 60),
    timeout: parseNumber(formData.timeout, 10),
    tags: formData.tags,
    workspace_id: workspaceID,
  }

  switch (formData.type) {
    case 'http':
      payload.http = {
        method: formData.httpMethod,
        valid_status: parseNumberList(formData.httpValidStatus, [200]),
        headers: {},
      }
      break
    case 'tcp':
      payload.tcp = {
        send: formData.tcpSend || undefined,
        expect_regex: formData.tcpExpectRegex || undefined,
      }
      break
    case 'udp':
      payload.udp = {
        send_hex: formData.udpSendHex || undefined,
        expect_contains: formData.udpExpectContains || undefined,
      }
      break
    case 'dns':
      payload.dns = {
        record_type: formData.dnsRecordType,
        expected: parseStringList(formData.dnsExpected),
      }
      break
    case 'icmp':
      payload.icmp = {
        count: parseNumber(formData.icmpCount, 4),
        interval: `${parseNumber(formData.icmpInterval, 1)}s`,
        max_loss_percent: parseNumber(formData.icmpMaxLossPercent, 100),
      }
      break
    case 'smtp':
      payload.smtp = {
        starttls: formData.smtpStartTLS,
        banner_contains: formData.smtpBannerContains || undefined,
      }
      break
    case 'imap':
      payload.imap = {
        tls: formData.imapTLS,
        check_mailbox: formData.imapMailbox || undefined,
        auth: formData.imapUsername ? { username: formData.imapUsername, password: formData.imapPassword } : undefined,
      }
      break
    case 'grpc':
      payload.grpc = {
        service: formData.grpcService || undefined,
        metadata: {},
      }
      break
    case 'websocket':
      payload.websocket = {
        headers: {},
        ping_check: formData.websocketPingCheck,
        send: formData.websocketSend || undefined,
        expect_contains: formData.websocketExpectContains || undefined,
      }
      break
    case 'tls':
      payload.tls = {
        expiry_warn_days: parseNumber(formData.tlsExpiryWarnDays, 30),
        expiry_critical_days: parseNumber(formData.tlsExpiryCriticalDays, 7),
      }
      break
  }

  return payload
}

const protocolKeys = ['http', 'tcp', 'udp', 'dns', 'icmp', 'smtp', 'imap', 'grpc', 'websocket', 'tls'] as const
const legacyProtocolKeys = ['http_config', 'tcp_config', 'dns_config'] as const
// Fields the server computes or owns; sending them back is pointless.
const serverManagedSoulKeys = ['id', 'created_at', 'updated_at', 'status', 'last_check', 'latency'] as const
// Maps the edit form never shows; an empty map from the form means
// "unchanged", not "delete every header".
const preservedMapKeys = ['headers', 'metadata'] as const

type SoulRecord = Record<string, unknown>

function isRecord(value: unknown): value is SoulRecord {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

/**
 * Layers an edit-form payload over the soul being edited.
 *
 * PUT /souls/:id replaces the whole record and the form only covers a
 * subset of fields, so saving used to drop everything else (body_regex,
 * json_path, DNS nameservers, TLS expected_issuer, auth, …). Redacted
 * secrets come back as their placeholder, which the server maps to the stored
 * value. When the type changes, the old protocol block is dropped.
 */
export function mergeSoulEdit(
  original: Soul,
  payload: ReturnType<typeof buildSoulPayload>
): ReturnType<typeof buildSoulPayload> {
  const base: SoulRecord = { ...(original as unknown as SoulRecord) }
  for (const key of serverManagedSoulKeys) delete base[key]
  const legacy: SoulRecord = {
    http: base.http ?? base.http_config,
    tcp: base.tcp ?? base.tcp_config,
    dns: base.dns ?? base.dns_config,
  }
  for (const key of legacyProtocolKeys) delete base[key]
  for (const key of protocolKeys) {
    if (key !== payload.type) delete base[key]
  }

  const merged: SoulRecord = { ...base, ...(payload as unknown as SoulRecord) }
  const key = payload.type
  const previous = original.type === payload.type ? (legacy[key] ?? base[key]) : undefined
  const next = (payload as unknown as SoulRecord)[key]
  if (isRecord(previous) && isRecord(next)) {
    const block: SoulRecord = { ...previous, ...next }
    for (const mapKey of preservedMapKeys) {
      const incoming = next[mapKey]
      if (isRecord(incoming) && Object.keys(incoming).length === 0 && isRecord(previous[mapKey])) {
        block[mapKey] = previous[mapKey]
      }
    }
    merged[key] = block
  }
  return merged as unknown as ReturnType<typeof buildSoulPayload>
}
