// Thin fetch wrapper for /api/v1. Every backend error is {code, message, params?}
// and is surfaced as ApiError so callers can localise by code.

export class ApiError extends Error {
  status: number
  code: string
  params: unknown[]
  constructor(status: number, code: string, message: string, params: unknown[] = []) {
    super(message)
    this.status = status
    this.code = code
    this.params = params
  }
}

export const API = '/api/v1'

type Listener = (err: ApiError) => void
const unauthorizedListeners = new Set<Listener>()
export function onUnauthorized(fn: Listener) {
  unauthorizedListeners.add(fn)
  return () => {
    unauthorizedListeners.delete(fn)
  }
}

export interface RequestOptions {
  method?: string
  body?: unknown
  query?: Record<string, string | number | boolean | undefined | null>
  headers?: Record<string, string>
  raw?: boolean // return Response instead of parsed JSON
  silent401?: boolean
}

export async function api<T = unknown>(path: string, opts: RequestOptions = {}): Promise<T> {
  const url = new URL((path.startsWith('/') ? '' : API + '/') + path, window.location.origin)
  if (path.startsWith('/')) url.pathname = path
  if (opts.query) {
    for (const [k, v] of Object.entries(opts.query)) {
      if (v !== undefined && v !== null && v !== '') url.searchParams.set(k, String(v))
    }
  }
  const headers: Record<string, string> = { Accept: 'application/json', ...(opts.headers ?? {}) }
  let body: BodyInit | undefined
  if (opts.body instanceof FormData) body = opts.body
  else if (opts.body !== undefined) {
    headers['Content-Type'] = 'application/json'
    body = JSON.stringify(opts.body)
  }
  const res = await fetch(url.toString(), { method: opts.method ?? 'GET', headers, body, credentials: 'same-origin' })
  if (opts.raw) return res as unknown as T
  if (res.status === 204) return undefined as T
  const text = await res.text()
  let data: any = undefined
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      data = { message: text }
    }
  }
  if (!res.ok) {
    const err = new ApiError(res.status, data?.code ?? `http.${res.status}`, data?.message ?? res.statusText, data?.params ?? [])
    if (res.status === 401 && !opts.silent401) unauthorizedListeners.forEach((fn) => fn(err))
    throw err
  }
  return data as T
}

export const get = <T,>(path: string, query?: RequestOptions['query']) => api<T>(path, { query })
export const post = <T,>(path: string, body?: unknown, query?: RequestOptions['query']) => api<T>(path, { method: 'POST', body, query })
export const put = <T,>(path: string, body?: unknown) => api<T>(path, { method: 'PUT', body })
export const del = <T,>(path: string) => api<T>(path, { method: 'DELETE' })
