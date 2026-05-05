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
    icmpInterval: Number.parseInt(soul.icmp?.interval || '1', 10) || 1,
    icmpMaxLossPercent: soul.icmp?.max_loss_percent ?? 100,
    smtpStartTLS: soul.smtp?.starttls ?? true,
    smtpBannerContains: soul.smtp?.banner_contains || '',
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
