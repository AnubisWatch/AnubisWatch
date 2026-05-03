const API_BASE_URL = '/api/v1'

export interface ApiResponse<T> {
  data: T
  pagination?: {
    total: number
    offset: number
    limit: number
    has_more: boolean
    next_offset?: number
  }
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

  const match = value.trim().match(/^(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)$/)
  if (!match) {
    return fallback
  }

  const amount = Number(match[1])
  const unit = match[2]
  switch (unit) {
    case 'ns':
      return amount / 1_000_000_000
    case 'us':
    case 'µs':
      return amount / 1_000_000
    case 'ms':
      return amount / 1_000
    case 'm':
      return amount * 60
    case 'h':
      return amount * 3600
    default:
      return amount
  }
}

function parseDurationMs(value: unknown): number {
  if (typeof value === 'number') {
    return Math.round(value / 1_000_000)
  }
  return Math.round(parseDurationSeconds(value) * 1000)
}

function normalizeJudgmentStatus(status: unknown): Judgment['status'] {
  switch (status) {
    case 'alive':
      return 'passed'
    case 'dead':
      return 'failed'
    case 'degraded':
    case 'unknown':
    case 'embalmed':
      return 'pending'
    default:
      return status === 'failed' || status === 'pending' ? status : 'passed'
  }
}

function normalizeSoulStatus(status: unknown): Soul['status'] {
  switch (status) {
    case 'alive':
    case 'healthy':
      return 'healthy'
    case 'dead':
    case 'unhealthy':
      return 'unhealthy'
    default:
      return 'unknown'
  }
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

  if ('id' in normalized && 'conditions' in normalized && 'channels' in normalized && 'enabled' in normalized) {
    normalizeAlertRule(normalized)
  }

  if ('id' in normalized && 'slug' in normalized && 'enabled' in normalized && 'souls' in normalized) {
    normalizeStatusPage(normalized)
  }

  return normalized
}

function normalizeStatusPage(page: Record<string, unknown>) {
  if (!page.domain && typeof page.custom_domain === 'string') {
    page.domain = page.custom_domain
  }
  if (isRecord(page.theme)) {
    const background = page.theme.background_color
    page.theme = background === '#ffffff' || background === '#f8fafc' ? 'light' : 'dark'
  }
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

function alertConditionToUICondition(condition: Record<string, unknown>): string {
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

  if ('condition' in serialized && 'threshold' in serialized && !('conditions' in serialized)) {
    serialized.conditions = [uiRuleToAlertCondition(serialized)]
    serialized.scope = serialized.scope || { type: 'all' }
    serialized.cooldown = serialized.cooldown || '5m'
    delete serialized.condition
    delete serialized.threshold
    delete serialized.duration
    delete serialized.consecutive
  }

  if ('slug' in serialized && 'souls' in serialized && 'theme' in serialized && typeof serialized.theme === 'string') {
    serialized.theme = statusPageThemeFromName(serialized.theme)
  }

  return serialized
}

function statusPageThemeFromName(theme: string): Record<string, string> {
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

function uiRuleToAlertCondition(rule: Record<string, unknown>): Record<string, unknown> {
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

function addDefaultCheckConfig(soul: Record<string, unknown>) {
  switch (soul.type) {
    case 'http':
      if (!soul.http) {
        soul.http = { method: 'GET', valid_status: [200], headers: {} }
      }
      break
    case 'tcp':
      if (!soul.tcp) {
        soul.tcp = {}
      }
      break
    case 'dns':
      if (!soul.dns) {
        soul.dns = { record_type: 'A' }
      }
      break
    case 'udp':
      if (!soul.udp) {
        soul.udp = {}
      }
      break
    case 'smtp':
      if (!soul.smtp) {
        soul.smtp = {}
      }
      break
    case 'icmp':
      if (!soul.icmp) {
        soul.icmp = { count: 4, interval: '1s', max_loss_percent: 100 }
      }
      break
    case 'grpc':
      if (!soul.grpc) {
        soul.grpc = { metadata: {} }
      }
      break
    case 'websocket':
      if (!soul.websocket) {
        soul.websocket = { headers: {}, ping_check: true }
      }
      break
    case 'tls':
      if (!soul.tls) {
        soul.tls = { expiry_warn_days: 30, expiry_critical_days: 7 }
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
  }

  clearToken() {
    this.token = null
    localStorage.removeItem('auth_token')
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
        this.clearToken()
        window.location.href = '/login'
      }
      const error = await response.json().catch(() => ({ error: 'Unknown error' }))
      throw new Error(error.error || `HTTP ${response.status}`)
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

  delete(endpoint: string): Promise<void> {
    return this.request<void>('DELETE', endpoint)
  }
}

export const api = new ApiClient()

// Types
export interface Soul {
  id: string
  name: string
  type: 'http' | 'tcp' | 'udp' | 'dns' | 'icmp' | 'smtp' | 'grpc' | 'websocket' | 'tls'
  target: string
  enabled: boolean
  weight: number
  timeout: number
  interval?: number
  tags?: string[]
  region?: string
  workspace_id?: string
  created_at?: string
  updated_at?: string
  status?: 'healthy' | 'unhealthy' | 'unknown'
  last_check?: string
  latency?: number
  http_config?: {
    method: string
    valid_status: number[]
    headers: Record<string, string>
    body?: string
  }
  tcp_config?: {
    tls: boolean
    tls_verify: boolean
  }
  dns_config?: {
    record_type: string
    expected_ips?: string[]
  }
}

export interface Judgment {
  id: string
  soul_id: string
  soul_name?: string
  status: 'passed' | 'failed' | 'pending'
  latency: number
  timestamp: string
  region: string
  error?: string
  purity?: number
}

export interface AlertChannel {
  id: string
  name: string
  type: 'email' | 'slack' | 'discord' | 'webhook' | 'pagerduty'
  enabled: boolean
  config: Record<string, string>
  created_at?: string
  updated_at?: string
}

export interface AlertRule {
  id: string
  name: string
  enabled: boolean
  condition: string
  threshold: number
  duration?: number
  consecutive?: number
  channels: string[]
  severity: 'critical' | 'warning' | 'info'
  created_at?: string
}

export interface Incident {
  id: string
  rule_id: string
  soul_id: string
  soul_name?: string
  workspace_id?: string
  status: 'open' | 'acknowledged' | 'resolved'
  severity: 'critical' | 'warning' | 'info'
  started_at: string
  acked_at?: string
  resolved_at?: string
  acked_by?: string
  resolved_by?: string
  escalation_level?: number
}

export interface Stats {
  souls?: {
    total: number
    healthy: number
    degraded: number
    dead: number
  }
  judgments?: {
    today: number
    failures: number
    avg_latency_ms: number
  }
  alerts?: {
    channels: number
    rules: number
    active_incidents: number
  }
}

export interface ClusterStatus {
  is_clustered: boolean
  node_id: string
  state: string
  leader?: string
  term?: number
  peer_count?: number
}

export interface StatusPage {
  id: string
  name: string
  slug: string
  enabled: boolean
  description?: string
  workspace_id?: string
  domain?: string
  custom_domain?: string
  theme?: 'dark' | 'light' | 'auto' | 'custom' | Record<string, string>
  souls?: string[]
  subscribers?: number
  created_at?: string
  updated_at?: string
}

export interface User {
  id: string
  email: string
  name: string
  role: string
  workspace: string
  created_at?: string
}

export interface CustomDashboard {
  id: string
  name: string
  description?: string
  widgets: WidgetConfig[]
  refresh_sec: number
  created_at?: string
  updated_at?: string
}

export interface WidgetConfig {
  id: string
  title: string
  type: 'line_chart' | 'bar_chart' | 'gauge' | 'stat' | 'table'
  grid: { x: number; y: number; width: number; height: number }
  query: { source: string; metric: string; filters?: Record<string, string>; time_range: string; aggregation?: string }
  thresholds?: { value: number; color: string; op: string }[]
}
