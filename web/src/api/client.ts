import {
  dispatchAuthSessionChanged,
  dispatchAuthTokenChanged,
} from './authEvents'
import type { components, paths } from './schema.generated'

type Schemas = components['schemas']

const API_BASE_URL = '/api/v1'

export interface ApiResponse<T> {
  data: T
  pagination?: Schemas['Pagination']
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function parseDurationSeconds(value: unknown, fallback = 0): number {
  if (typeof value === 'number') {
    return value
  }
  if (typeof value !== 'string') {
    return fallback
  }

  const input = value.trim()
  const partPattern = /(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/g
  let total = 0
  let consumed = ''
  for (const match of input.matchAll(partPattern)) {
    const amount = Number(match[1])
    const unit = match[2]
    consumed += match[0]
    switch (unit) {
      case 'ns':
        total += amount / 1_000_000_000
        break
      case 'us':
      case 'µs':
        total += amount / 1_000_000
        break
      case 'ms':
        total += amount / 1_000
        break
      case 'm':
        total += amount * 60
        break
      case 'h':
        total += amount * 3600
        break
      default:
        total += amount
    }
  }

  if (consumed !== input || consumed === '') {
    return fallback
  }

  return total
}

function parseDurationMs(value: unknown): number {
  if (typeof value === 'number') {
    return Math.round(value / 1_000_000)
  }
  return Math.round(parseDurationSeconds(value) * 1000)
}

function hasKey<K extends string>(table: Record<K, unknown>, key: unknown): key is K {
  return typeof key === 'string' && Object.prototype.hasOwnProperty.call(table, key)
}

// Presentation mapping of the API's judgment status (core.SoulStatus) to the
// UI vocabulary. `satisfies` makes a status added to the spec a compile error
// until it is mapped here.
const judgmentStatusLabels = {
  alive: 'passed',
  dead: 'failed',
  degraded: 'degraded',
  embalmed: 'maintenance',
  unknown: 'pending',
} as const satisfies Record<ApiJudgment['status'], JudgmentDisplayStatus>

// status is an ApiJudgment['status'], or a value normalised earlier.
function normalizeJudgmentStatus(status: unknown): JudgmentDisplayStatus {
  if (hasKey(judgmentStatusLabels, status)) {
    return judgmentStatusLabels[status]
  }
  // Already normalised (a cached value fed back through the client).
  return status === 'failed' || status === 'pending' || status === 'maintenance' ? status : 'passed'
}

// Presentation mapping of the soul status the API reports (alive/dead are
// already renamed healthy/unhealthy server-side) to the UI vocabulary.
// Degraded and embalmed used to collapse into unknown, hiding slow services
// and maintenance.
const soulStatusLabels = {
  healthy: 'healthy',
  unhealthy: 'unhealthy',
  degraded: 'degraded',
  embalmed: 'maintenance',
  unknown: 'unknown',
} as const satisfies Record<NonNullable<ApiSoul['status']>, SoulDisplayStatus>

// Raw judgment statuses (live events) and already-normalised values.
const soulStatusAliases = {
  alive: 'healthy',
  dead: 'unhealthy',
  maintenance: 'maintenance',
} as const satisfies Partial<Record<ApiJudgment['status'] | SoulDisplayStatus, SoulDisplayStatus>>

// status is an ApiSoul['status'], a raw ApiJudgment['status'] or a value
// normalised earlier.
function normalizeSoulStatus(status: unknown): SoulDisplayStatus {
  if (hasKey(soulStatusLabels, status)) {
    return soulStatusLabels[status]
  }
  if (hasKey(soulStatusAliases, status)) {
    return soulStatusAliases[status]
  }
  return 'unknown'
}

function normalizeApiValue(value: unknown): unknown {
  if (Array.isArray(value)) {
    return value.map(normalizeApiValue)
  }

  if (!isRecord(value)) {
    return value
  }

  const normalized: Record<string, unknown> = {}
  for (const [key, child] of Object.entries(value)) {
    normalized[key] = normalizeApiValue(child)
  }

  if ('data' in normalized && ('pagination' in normalized || Array.isArray(normalized.data))) {
    normalized.data = normalizeApiValue(normalized.data)
    return normalized
  }

  if ('soul_id' in normalized && 'status' in normalized && 'duration' in normalized) {
    normalized.status = normalizeJudgmentStatus(normalized.status)
    normalized.latency = parseDurationMs(normalized.duration)
    if (!normalized.error && typeof normalized.message === 'string') {
      normalized.error = normalized.message
    }
  }

  if ('id' in normalized && 'type' in normalized && 'target' in normalized) {
    normalized.weight = parseDurationSeconds(normalized.weight, 60)
    normalized.timeout = parseDurationSeconds(normalized.timeout, 10)
    if ('status' in normalized) {
      normalized.status = normalizeSoulStatus(normalized.status)
    }
    if (!normalized.http_config && normalized.http) {
      normalized.http_config = normalized.http
    }
    if (!normalized.tcp_config && normalized.tcp) {
      normalized.tcp_config = normalized.tcp
    }
    if (!normalized.dns_config && normalized.dns) {
      normalized.dns_config = normalized.dns
    }
  }

  if ('id' in normalized && 'steps' in normalized && ('weight' in normalized || 'timeout' in normalized)) {
    normalized.weight = parseDurationSeconds(normalized.weight, 60)
    normalized.timeout = parseDurationSeconds(normalized.timeout, 30)
    // Steps have no id, so they skip the soul normalisation above; their
    // timeouts arrive as "10s" and the edit form needs seconds.
    if (Array.isArray(normalized.steps)) {
      normalized.steps = normalized.steps.map((step) =>
        isRecord(step) ? { ...step, timeout: parseDurationSeconds(step.timeout, 10) } : step
      )
    }
  }

  if ('id' in normalized && 'conditions' in normalized && 'channels' in normalized && 'enabled' in normalized) {
    normalizeAlertRule(normalized)
  }

  if ('id' in normalized && 'slug' in normalized && 'enabled' in normalized && 'souls' in normalized) {
    normalizeStatusPage(normalized)
  }

  // A custom dashboard: servers before the widgets fix sent widgets: null for
  // a dashboard saved without widgets; the pages iterate the array.
  if ('id' in normalized && 'widgets' in normalized && 'refresh_sec' in normalized && !Array.isArray(normalized.widgets)) {
    normalized.widgets = []
  }

  return normalized
}

function normalizeStatusPage(page: Record<string, unknown>) {
  if (!page.domain && typeof page.custom_domain === 'string') {
    page.domain = page.custom_domain
  }
  if (isRecord(page.theme)) {
    // Keep the stored colours so saving the page does not reset a custom theme.
    page.theme_config = page.theme
    page.theme = statusPageThemeName(page.theme)
  }
}

function statusPageThemeName(theme: Partial<StatusPageTheme>): 'dark' | 'light' | 'custom' {
  const background = theme.background_color
  if (background === '#ffffff' || background === '#f8fafc') return 'light'
  if (!background || background === '#0f172a') return 'dark'
  return 'custom'
}

function normalizeAlertRule(rule: Record<string, unknown>) {
  const conditions = Array.isArray(rule.conditions) ? rule.conditions : []
  const firstCondition = isRecord(conditions[0]) ? conditions[0] : {}

  if (!rule.condition && firstCondition.type) {
    rule.condition = alertConditionToUICondition(firstCondition)
  }
  if (rule.threshold === undefined) {
    const value = firstCondition.value ?? firstCondition.threshold
    rule.threshold = typeof value === 'number' ? value : Number(value || 0)
  }
  if (rule.duration === undefined) {
    rule.duration = parseDurationSeconds(firstCondition.window ?? firstCondition.duration, 0)
  }
  if (rule.consecutive === undefined && firstCondition.type === 'consecutive_failures') {
    rule.consecutive = typeof firstCondition.threshold === 'number' ? firstCondition.threshold : Number(firstCondition.threshold || 1)
  }
  if (!rule.severity) {
    rule.severity = 'warning'
  }
}

function alertConditionToUICondition(condition: Partial<Schemas['AlertCondition']>): string {
  if (condition.type === 'failure_rate') {
    return 'error_rate'
  }
  if (condition.type === 'consecutive_failures') {
    return 'downtime'
  }
  if (condition.metric === 'tls_expiry_days') {
    return 'ssl_expiry'
  }
  return 'response_time'
}

function serializeApiBody(body: unknown): unknown {
  if (Array.isArray(body)) {
    return body.map(serializeApiBody)
  }
  if (!isRecord(body)) {
    return body
  }

  const serialized: Record<string, unknown> = {}
  for (const [key, value] of Object.entries(body)) {
    serialized[key] = serializeApiBody(value)
  }

  // Souls and journey steps (both carry type and target). Protocol defaults
  // only fill a missing block, so a step's stored http method/body is kept.
  if ('type' in serialized && 'target' in serialized) {
    if (typeof serialized.weight === 'number') {
      serialized.weight = `${serialized.weight}s`
    }
    if (typeof serialized.timeout === 'number') {
      serialized.timeout = `${serialized.timeout}s`
    }
    if (serialized.http_config && !serialized.http) {
      serialized.http = serialized.http_config
    }
    if (serialized.tcp_config && !serialized.tcp) {
      serialized.tcp = serialized.tcp_config
    }
    if (serialized.dns_config && !serialized.dns) {
      serialized.dns = serialized.dns_config
    }
    delete serialized.http_config
    delete serialized.tcp_config
    delete serialized.dns_config
    addDefaultCheckConfig(serialized)
  }

  if ('steps' in serialized && ('weight' in serialized || 'timeout' in serialized)) {
    if (typeof serialized.weight === 'number') {
      serialized.weight = `${serialized.weight}s`
    }
    if (typeof serialized.timeout === 'number') {
      serialized.timeout = `${serialized.timeout}s`
    }
  }

  if ('condition' in serialized && 'threshold' in serialized) {
    // The form edits the primary condition only; any further conditions of a
    // rule created through the API or config file are kept as they were.
    const rest = Array.isArray(serialized.conditions) ? serialized.conditions.slice(1) : []
    serialized.conditions = [uiRuleToAlertCondition(serialized), ...rest]
    serialized.scope = serialized.scope || { type: 'all' }
    serialized.cooldown = serialized.cooldown || '5m'
    delete serialized.condition
    delete serialized.threshold
    delete serialized.duration
    delete serialized.consecutive
  }

  if ('slug' in serialized && 'souls' in serialized && 'theme' in serialized && typeof serialized.theme === 'string') {
    const stored = serialized.theme_config
    serialized.theme = isRecord(stored) && (serialized.theme === 'custom' || statusPageThemeName(stored) === serialized.theme)
      ? stored
      : statusPageThemeFromName(serialized.theme)
  }
  delete serialized.theme_config

  return serialized
}

function statusPageThemeFromName(theme: string): StatusPageTheme {
  if (theme === 'light') {
    return {
      primary_color: '#d97706',
      background_color: '#ffffff',
      text_color: '#111827',
      accent_color: '#0d9488',
      font_family: 'system-ui, -apple-system, sans-serif',
    }
  }

  return {
    primary_color: '#fbbf24',
    background_color: '#0f172a',
    text_color: '#f8fafc',
    accent_color: '#14b8a6',
    font_family: 'system-ui, -apple-system, sans-serif',
  }
}

function uiRuleToAlertCondition(rule: Record<string, unknown>): Schemas['AlertCondition'] {
  const threshold = typeof rule.threshold === 'number' ? rule.threshold : Number(rule.threshold || 0)
  const duration = typeof rule.duration === 'number' ? rule.duration : Number(rule.duration || 60)
  const consecutive = typeof rule.consecutive === 'number' ? rule.consecutive : Number(rule.consecutive || 1)

  switch (rule.condition) {
    case 'error_rate':
      return { type: 'failure_rate', threshold, window: `${duration}s` }
    case 'downtime':
      return { type: 'consecutive_failures', threshold: consecutive, status: 'dead', window: `${duration}s` }
    case 'ssl_expiry':
      return { type: 'threshold', metric: 'tls_expiry_days', operator: '<', value: threshold, window: `${duration}s` }
    case 'response_time':
    default:
      return { type: 'threshold', metric: 'latency_ms', operator: '>', value: threshold, window: `${duration}s` }
  }
}

// Protocol defaults for a soul or journey step; each literal is checked
// against the spec's config schema.
function addDefaultCheckConfig(soul: Record<string, unknown>) {
  switch (soul.type as Schemas['SoulType']) {
    case 'http':
      if (!soul.http) {
        soul.http = { method: 'GET', valid_status: [200], headers: {} } satisfies Schemas['HTTPConfig']
      }
      break
    case 'tcp':
      if (!soul.tcp) {
        soul.tcp = {} satisfies Schemas['TCPConfig']
      }
      break
    case 'dns':
      if (!soul.dns) {
        soul.dns = { record_type: 'A' } satisfies Schemas['DNSConfig']
      }
      break
    case 'udp':
      if (!soul.udp) {
        soul.udp = {} satisfies Schemas['UDPConfig']
      }
      break
    case 'smtp':
      if (!soul.smtp) {
        soul.smtp = {} satisfies Schemas['SMTPConfig']
      }
      break
    case 'imap':
      if (!soul.imap) {
        soul.imap = { tls: true } satisfies Schemas['IMAPConfig']
      }
      break
    case 'icmp':
      if (!soul.icmp) {
        soul.icmp = { count: 4, interval: '1s', max_loss_percent: 100 } satisfies Schemas['ICMPConfig']
      }
      break
    case 'grpc':
      if (!soul.grpc) {
        soul.grpc = { metadata: {} } satisfies Schemas['GRPCConfig']
      }
      break
    case 'websocket':
      if (!soul.websocket) {
        soul.websocket = { headers: {}, ping_check: true } satisfies Schemas['WebSocketConfig']
      }
      break
    case 'tls':
      if (!soul.tls) {
        soul.tls = { expiry_warn_days: 30, expiry_critical_days: 7 } satisfies Schemas['TLSConfig']
      }
      break
    case 'heartbeat':
      if (!soul.heartbeat) {
        soul.heartbeat = {} satisfies Schemas['HeartbeatConfig']
      }
      break
  }
}

class ApiClient {
  private baseUrl: string
  private token: string | null

  constructor(baseUrl: string = API_BASE_URL) {
    this.baseUrl = baseUrl
    // SECURITY: Prefer httpOnly cookie over localStorage (VULN-004 fix)
    // Token is kept for WebSocket connections and backward compatibility
    this.token = localStorage.getItem('auth_token')
  }

  setToken(token: string) {
    this.token = token
    // Keep localStorage for WebSocket compatibility
    // The actual session is stored in httpOnly cookie by backend
    localStorage.setItem('auth_token', token)
    dispatchAuthTokenChanged()
  }

  clearToken(notify = true) {
    this.token = null
    localStorage.removeItem('auth_token')
    if (notify) {
      dispatchAuthTokenChanged()
    }
  }

  private async request<T>(
    method: string,
    endpoint: string,
    body?: unknown
  ): Promise<T> {
    const url = `${this.baseUrl}${endpoint}`
    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
    }

    // Include Authorization header for WebSocket compatibility
    // Backend also checks httpOnly cookie for security
    if (this.token) {
      headers['Authorization'] = `Bearer ${this.token}`
    }

    const options: RequestInit = {
      method,
      headers,
      // SECURITY: Include credentials (cookies) in requests (VULN-004 fix)
      credentials: 'include',
    }

    if (body) {
      options.body = JSON.stringify(serializeApiBody(body))
    }

    const response = await fetch(url, options)

    if (!response.ok) {
      if (response.status === 401) {
        this.token = null
        localStorage.removeItem('auth_token')
        dispatchAuthSessionChanged({ state: 'anonymous' })
        if (endpoint !== '/auth/me' && endpoint !== '/auth/login') {
          window.location.href = '/login'
        }
      }
      const error = await response.json().catch(() => ({ error: 'Unknown error' }))
      throw new ApiError(response.status, error)
    }

    if (response.status === 204) {
      return undefined as unknown as T
    }

    const data = await response.json()
    return normalizeApiValue(data) as T
  }

  get<T>(endpoint: string): Promise<T> {
    return this.request<T>('GET', endpoint)
  }

  post<T>(endpoint: string, body?: unknown): Promise<T> {
    return this.request<T>('POST', endpoint, body)
  }

  put<T>(endpoint: string, body?: unknown): Promise<T> {
    return this.request<T>('PUT', endpoint, body)
  }

  patch<T>(endpoint: string, body?: unknown): Promise<T> {
    return this.request<T>('PATCH', endpoint, body)
  }

  delete(endpoint: string): Promise<void> {
    return this.request<void>('DELETE', endpoint)
  }
}

export const api = new ApiClient()

/**
 * A failed API request. It is still an Error whose message is the server's
 * `error` text; status and the structured fields of the body (for example a
 * workspace quota refusal: code "quota_exceeded", quota, limit) ride along.
 */
export class ApiError extends Error {
  readonly status: number
  readonly code?: string
  readonly quota?: string
  readonly limit?: number | string
  readonly current?: number

  constructor(status: number, body: { error?: string; code?: unknown; quota?: unknown; limit?: unknown; current?: unknown } = {}) {
    super(body.error || `HTTP ${status}`)
    this.name = 'ApiError'
    this.status = status
    if (typeof body.code === 'string') this.code = body.code
    if (typeof body.quota === 'string') this.quota = body.quota
    if (typeof body.limit === 'number' || typeof body.limit === 'string') this.limit = body.limit
    if (typeof body.current === 'number') this.current = body.current
  }
}

// Types
//
// Wire types come from the OpenAPI spec (internal/api/openapi.yaml) through
// the generated ./schema.generated.ts, so a backend change that renames or
// retypes a field fails `tsc` here instead of drifting silently. Where the
// dashboard reshapes a response for presentation (normalizeApiValue above:
// durations to seconds, alive/dead to the UI status vocabulary), the exported
// type is the generated one with exactly those fields replaced.

/** Wire format of a soul (GET /souls, /souls/:id). */
export type ApiSoul = Schemas['Soul']
/** Wire format of a judgment. */
export type ApiJudgment = Schemas['Judgment']
/** Wire format of an alert rule. */
export type ApiAlertRule = Schemas['AlertRule']
/** Wire format of a status page. */
export type ApiStatusPage = Schemas['StatusPage']
/** GET /api/v1/branding (public): dashboard.branding with defaults filled in. */
export type DashboardBranding = Schemas['DashboardBranding']

export type QuotaCounter = Schemas['QuotaCounter']
/** GET /api/v1/quota: the current workspace's usage against its quotas. */
export type QuotaUsage = Schemas['QuotaUsage']
/** GET /api/v1/api-keys item. The secret and its hash are never returned. */
export type ApiKey = Schemas['APIKey']
/** POST /api/v1/api-keys response: `key` is the plaintext, shown once. */
export type CreatedApiKey = Schemas['APIKeyCreated']
/** GET /api/v1/members item: an account's role in the current workspace. */
export type Member = Schemas['Member']
/** GET /api/v1/invitations item. The token is never returned. */
export type Invitation = Schemas['Invitation']
/**
 * POST /api/v1/invitations response. When the link could not be emailed,
 * invite_url carries it this one time.
 */
export type CreatedInvitation = Schemas['InvitationCreated']

/** UI status vocabulary for souls (normalizeSoulStatus). */
export type SoulDisplayStatus = 'healthy' | 'unhealthy' | 'degraded' | 'maintenance' | 'unknown'

/**
 * A soul as the dashboard holds it: the wire soul with weight and timeout in
 * seconds, status in the UI vocabulary, and the http/tcp/dns blocks mirrored
 * under the form's *_config names.
 */
export type Soul = Omit<ApiSoul, 'weight' | 'timeout' | 'status' | 'workspace_id'> & {
  /** Check interval in seconds (the API sends a duration string). */
  weight: number
  /** Per-check timeout in seconds (the API sends a duration string). */
  timeout: number
  status?: SoulDisplayStatus
  workspace_id?: string
  http_config?: ApiSoul['http']
  tcp_config?: ApiSoul['tcp']
  dns_config?: ApiSoul['dns']
}

export type SoulFlapDetection = Schemas['FlapDetection']

/** Ping record of a heartbeat soul (GET /souls/:id/heartbeat `state`). */
export type HeartbeatState = NonNullable<Schemas['HeartbeatState']>
/** GET /souls/:id/heartbeat and POST /souls/:id/heartbeat/rotate. */
export type HeartbeatInfo = Schemas['HeartbeatInfo']

/** UI status vocabulary for judgments (normalizeJudgmentStatus). */
export type JudgmentDisplayStatus = 'passed' | 'failed' | 'degraded' | 'maintenance' | 'pending'

/**
 * A judgment as the dashboard holds it: status in the UI vocabulary, latency
 * in milliseconds (from duration) and error (from message).
 */
export type Judgment = Omit<ApiJudgment, 'status'> & {
  status: JudgmentDisplayStatus
  /** Milliseconds, derived from duration (nanoseconds). */
  latency: number
  /** The failure reason, copied from message. */
  error?: string
}

/** One period of GET /souls/:id/uptime. uptime_percent is null when there were no checks. */
export type UptimeWindow = Schemas['UptimeWindow']
/**
 * GET /souls/:id/uptime. Series buckets are equal over the requested range,
 * oldest first: 5 minutes for 1h, hours for 24h, calendar days (in the
 * caller's time zone) for 7d, 30d, 90d and 1y.
 */
export type SoulUptime = Schemas['SoulUptime']
export type UptimeRange = SoulUptime['series']['range']

export type AlertChannel = Schemas['AlertChannel']

/**
 * An alert rule as the dashboard holds it: the wire rule plus the primary
 * condition flattened into the form's condition/threshold/duration fields
 * (normalizeAlertRule; serializeApiBody folds them back into conditions).
 */
export type AlertRule = ApiAlertRule & {
  condition: string
  threshold: number
  /** Seconds. */
  duration?: number
  consecutive?: number
}

export type Incident = Schemas['Incident']

/** GET /api/v1/stats/overview. */
export type Stats = paths['/api/v1/stats/overview']['get']['responses'][200]['content']['application/json']

export type ClusterStatus = Schemas['ClusterStatus']

export type StatusPageVisibility = NonNullable<ApiStatusPage['visibility']>
export type StatusPageTheme = Schemas['StatusPageTheme']

/**
 * A status page as the dashboard holds it: theme reduced to a preset name with
 * the stored colours kept in theme_config, and custom_domain mirrored as domain.
 */
export type StatusPage = Omit<ApiStatusPage, 'theme'> & {
  theme?: 'dark' | 'light' | 'custom' | StatusPageTheme
  /** The stored theme colours, kept alongside the derived theme name. */
  theme_config?: StatusPageTheme
  domain?: string
  /**
   * Write-only: the API hashes this and never returns it. Send it only when the
   * password is being set or changed; a present-but-empty value clears it.
   */
  password?: string
}

/**
 * A journey step as the dashboard holds it: timeout in seconds (the API sends
 * a duration string; normalizeApiValue converts it, defaulting to 10).
 */
export type JourneyStep = Omit<Schemas['JourneyStep'], 'timeout'> & { timeout: number }
/**
 * A journey as GET /journeys lists it (with run statistics), weight and
 * timeout in seconds.
 */
export type Journey = Omit<Schemas['JourneyListItem'], 'weight' | 'timeout' | 'steps'> & {
  weight: number
  timeout: number
  steps: JourneyStep[]
}
export type JourneyRun = Schemas['JourneyRun']
export type JourneyStepResult = Schemas['JourneyStepResult']

export type StatusPageIncident = Schemas['StatusIncident']
export type StatusPageIncidentUpdate = NonNullable<StatusPageIncident['updates']>[number]
export type StatusPageAnnouncement = Schemas['StatusAnnouncement']
export type IncidentImpact = NonNullable<StatusPageIncident['impact']>
export type IncidentStatus = StatusPageIncident['status']
export type MaintenanceWindow = Schemas['MaintenanceWindow']
export type MaintenanceWindowCreate = Schemas['MaintenanceWindowCreate']
export type MaintenanceWindowUpdate = Schemas['MaintenanceWindowUpdate']
/** GET/PUT /api/v1/config: the Settings page's view of the configuration. */
export type UIConfig = Schemas['UIConfig']
/** GET /api/v1/auth/methods (public). */
export type AuthMethods = paths['/api/v1/auth/methods']['get']['responses'][200]['content']['application/json']

export type User = Schemas['User']
export type Workspace = Schemas['Workspace']
/** A widget query as stored: the server always encodes metric and time_range. */
export type WidgetQuery = Schemas['WidgetQuery'] & Required<Pick<Schemas['WidgetQuery'], 'metric' | 'time_range'>>
export type WidgetConfig = Omit<Schemas['WidgetConfig'], 'query'> & { query: WidgetQuery }
/**
 * A custom dashboard. widgets is always an array: the API sends [] for a
 * dashboard without widgets, and normalizeApiValue replaces a null from an
 * older server.
 */
export type CustomDashboard = Omit<Schemas['CustomDashboard'], 'widgets'> & { widgets: WidgetConfig[] }

export type HttpMethod = NonNullable<Schemas['HTTPConfig']['method']>
export type DnsRecordType = NonNullable<Schemas['DNSConfig']['record_type']>
