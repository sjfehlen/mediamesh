const BASE_URL = import.meta.env.VITE_API_BASE_URL ?? ''

export function getToken(): string | null {
  return localStorage.getItem('session_token')
}

export function setToken(token: string): void {
  localStorage.setItem('session_token', token)
}

export function clearToken(): void {
  localStorage.removeItem('session_token')
}

class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

async function request<T>(
  method: string,
  path: string,
  body?: unknown,
): Promise<T> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
  }
  const token = getToken()
  if (token) {
    headers['Authorization'] = `Bearer ${token}`
  }

  const res = await fetch(`${BASE_URL}${path}`, {
    method,
    headers,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })

  if (!res.ok) {
    const text = await res.text()
    throw new ApiError(res.status, text.trim() || res.statusText)
  }

  if (res.status === 204 || res.headers.get('content-length') === '0') {
    return undefined as T
  }

  return res.json() as Promise<T>
}

export const api = {
  get: <T>(path: string) => request<T>('GET', path),
  post: <T>(path: string, body?: unknown) => request<T>('POST', path, body),
  patch: <T>(path: string, body?: unknown) => request<T>('PATCH', path, body),
  delete: <T>(path: string) => request<T>('DELETE', path),
}

// ---- Typed helpers ----

export interface LoginResponse {
  token: string
}

export interface LibraryItem {
  id: string
  peer_id?: string
  media_type: string
  title: string
  meta_title?: string
  year?: number
  series?: string
  season_num?: number
  episode_num?: number
  relative_path: string
  file_size?: number
  poster_url?: string
  description?: string
  rating?: number
  genres?: string
}

export interface Request {
  id: string
  user_id: string
  item_id: string
  status: 'pending' | 'approved' | 'rejected'
  note?: string
  reviewed_by?: string
  review_note?: string
  requested_at: string
  reviewed_at?: string
}

export interface Transfer {
  id: string
  request_id: string
  peer_id: string
  item_id: string
  status: 'queued' | 'active' | 'paused' | 'complete' | 'failed'
  bytes_total?: number
  bytes_done: number
  error?: string
  queued_at: string
  started_at?: string
  completed_at?: string
}

export interface Peer {
  id: string
  display_name: string
  endpoint: string
  status: string
  added_at: string
}

export interface User {
  id: string
  username: string
  display_name: string
  role: string
  auto_approve: boolean
  can_request: boolean
  quota_gb?: number
  created_at: string
  disabled_at?: string
}

export interface AuditEntry {
  id: string
  actor_id: string
  actor_type: string
  action: string
  target_type: string
  target_id: string
  detail: string
  occurred_at: string
}

export const getItem = (id: string) => api.get<LibraryItem>(`/api/library/${id}`)
export const patchItem = (id: string, patch: Partial<Pick<LibraryItem, 'meta_title' | 'description' | 'poster_url' | 'rating'>>) =>
  api.patch<LibraryItem>(`/api/library/${id}`, patch)

export const getPeers = () => api.get<Peer[]>('/api/peers')

export const submitRequest = (itemId: string, note?: string) =>
  api.post<Request>('/api/requests', { item_id: itemId, note })
