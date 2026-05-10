import axios, { AxiosInstance, AxiosResponse } from 'axios'
import type { ApiResponse } from '@/types'

const ideRootClient: AxiosInstance = axios.create({
  baseURL: '',
  withCredentials: true,
  timeout: 30000,
  headers: {
    'Content-Type': 'application/json'
  }
})

ideRootClient.interceptors.request.use((config) => {
  const token = localStorage.getItem('auth_token')
  if (token && config.headers) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

ideRootClient.interceptors.response.use((response: AxiosResponse) => {
  const apiResponse = response.data as ApiResponse<unknown>
  if (apiResponse && typeof apiResponse === 'object' && 'code' in apiResponse) {
    if (apiResponse.code === 0) {
      response.data = apiResponse.data
      return response
    }
    return Promise.reject({
      status: response.status,
      code: apiResponse.code,
      message: apiResponse.message || 'Unknown error'
    })
  }
  return response
})

export interface IDEDownloadInfo {
  url?: string
  sha256?: string
  size?: number | null
}

export interface IDEVersionInfo {
  kind: 'app' | 'engine'
  version?: string
  latest_version?: string
  current_version?: string
  has_update?: boolean
  is_mandatory?: boolean
  min_app_version?: string
  published_at?: string
  download_url?: string
  manifest_url?: string
  sha256?: string
  release_notes?: string
  download?: IDEDownloadInfo
  binaries?: Record<string, IDEDownloadInfo>
}

export interface IDESessionInfo {
  id: string
  user_id: number
  user_email?: string
  client_id: string
  client_version?: string
  platform?: string
  device_id?: string
  expires_at: string
  last_used_at: string
  created_at: string
  revoked: boolean
  revoke_reason?: string
}

export interface IDESessionsResponse {
  items: IDESessionInfo[]
  total: number
}

export interface IDEStatsResponse {
  active_sessions: number
  revoked_sessions: number
  total_sessions: number
}

export interface IDEPublishReleaseRequest {
  kind: 'app' | 'engine'
  version: string
  min_app_version?: string
  binaries: Record<string, IDEDownloadInfo>
  release_notes?: string
  is_mandatory?: boolean
}

export interface IDEReleasesResponse {
  items: IDEVersionInfo[]
  total: number
}

export interface IDETelemetryEvent {
  type: string
  timestamp?: string
  data?: Record<string, unknown>
}

export async function getVersion(
  kind: 'app' | 'engine',
  params: { current?: string; platform?: string; arch?: string } = {}
): Promise<IDEVersionInfo> {
  const { data } = await ideRootClient.get<IDEVersionInfo>(`/ide/api/version/${kind}`, { params })
  return data
}

export async function reportTelemetry(events: IDETelemetryEvent[]): Promise<void> {
  await ideRootClient.post('/ide/api/telemetry', { events })
}

export async function listSessions(): Promise<IDESessionsResponse> {
  const { data } = await ideRootClient.get<IDESessionsResponse>('/api/v1/admin/ide/sessions')
  return data
}

export async function revokeSession(id: string): Promise<void> {
  await ideRootClient.post(`/api/v1/admin/ide/sessions/${encodeURIComponent(id)}/revoke`)
}

export async function getStats(): Promise<IDEStatsResponse> {
  const { data } = await ideRootClient.get<IDEStatsResponse>('/api/v1/admin/ide/stats')
  return data
}

export async function listReleases(): Promise<IDEReleasesResponse> {
  const { data } = await ideRootClient.get<IDEReleasesResponse>('/api/v1/admin/ide/releases')
  return data
}

export async function publishRelease(request: IDEPublishReleaseRequest): Promise<IDEVersionInfo> {
  const { data } = await ideRootClient.post<IDEVersionInfo>('/api/v1/admin/ide/releases', request)
  return data
}

export default {
  getVersion,
  reportTelemetry,
  listSessions,
  revokeSession,
  getStats,
  listReleases,
  publishRelease,
}
