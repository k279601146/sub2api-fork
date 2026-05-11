import { apiClient } from './client'

export interface IDEAuthorizeParams {
  code_challenge: string
  code_challenge_method?: string
  redirect_uri: string
  client_id?: string
}

export interface IDEAuthorizeResponse {
  state: string
  login_url: string
  redirect_uri: string
  expires_at: string
}

export interface IDEApproveResponse {
  redirect_url: string
  expires_at: string
}

export async function authorizeIDELogin(
  params: IDEAuthorizeParams
): Promise<IDEAuthorizeResponse> {
  const { data } = await apiClient.get<IDEAuthorizeResponse>('/ide/auth/authorize', {
    params: {
      ...params,
      response_mode: 'json'
    },
    headers: {
      Accept: 'application/json'
    }
  })
  return data
}

export async function approveIDELogin(state: string): Promise<IDEApproveResponse> {
  const { data } = await apiClient.post<IDEApproveResponse>('/ide/auth/approve', { state })
  return data
}

export function buildIDECancelRedirect(redirectURI: string): string {
  const url = new URL(redirectURI)
  url.searchParams.set('error', 'access_denied')
  return url.toString()
}

