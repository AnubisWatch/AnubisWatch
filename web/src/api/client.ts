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

export interface ApiError {
  error: string
}

class ApiClient {
  private baseUrl: string
  private token: string | null

  constructor(baseUrl: string = API_BASE_URL) {
    this.baseUrl = baseUrl
    this.token = localStorage.getItem('auth_token')
  }

  setToken(token: string) {
    this.token = token
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

    if (this.token) {
      headers['Authorization'] = `Bearer ${this.token}`
    }

    const options: RequestInit = {
      method,
      headers,
    }

    if (body) {
      options.body = JSON.stringify(body)
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
      return null as T
    }

    return response.json()
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

  delete<T>(endpoint: string): Promise<T> {
    return this.request<T>('DELETE', endpoint)
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
  condition: 'down' | 'degraded' | 'latency_spike'
  threshold: number
  duration: number
  channels: string[]
  severity: 'critical' | 'warning' | 'info'
  created_at?: string
}

export interface Workspace {
  id: string
  name: string
  description?: string
  created_at?: string
  updated_at?: string
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
  theme?: 'dark' | 'light' | 'custom'
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
