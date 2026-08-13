import { afterEach, describe, expect, it, vi } from 'vitest'
import { NextRequest } from 'next/server'

import { GET, POST } from './route'

describe('API proxy route transport', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllEnvs()
  })

  it('preserves the method, body, and critical headers for an SSE response', async () => {
    vi.stubEnv('AGENTAPI_PROXY_URL', 'http://backend:8080')
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response('data: {"ok":true}\n\n', {
        status: 202,
        headers: {
          'Content-Type': 'text/event-stream',
          ETag: '"stream"',
        },
      }),
    )
    const request = new NextRequest('https://ui.example.test/api/proxy/s/share-token/events?cursor=1', {
      method: 'POST',
      headers: {
        Accept: 'text/event-stream',
        'Content-Type': 'application/json-patch+json',
        'Last-Event-ID': '9',
      },
      body: '{"operation":"subscribe"}',
    })

    const response = await POST(request, {
      params: Promise.resolve({ path: ['s', 'share-token', 'events'] }),
    })

    expect(fetchMock).toHaveBeenCalledOnce()
    const [url, options] = fetchMock.mock.calls[0]
    const headers = new Headers(options?.headers)
    expect(url).toBe('http://backend:8080/s/share-token/events?cursor=1')
    expect(options?.method).toBe('POST')
    expect(new TextDecoder().decode(options?.body as ArrayBuffer)).toBe('{"operation":"subscribe"}')
    expect(headers.get('accept')).toBe('text/event-stream')
    expect(headers.get('content-type')).toBe('application/json-patch+json')
    expect(headers.get('last-event-id')).toBe('9')
    expect(response.status).toBe(202)
    expect(response.headers.get('etag')).toBe('"stream"')
    expect(await response.text()).toBe(': connected\n\ndata: {"ok":true}\n\n')
  })

  it('aborts the upstream stream when the downstream reader cancels', async () => {
    vi.stubEnv('AGENTAPI_PROXY_URL', 'http://backend:8080')
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(new ReadableStream<Uint8Array>(), {
        headers: { 'Content-Type': 'text/event-stream' },
      }),
    )
    const request = new NextRequest('https://ui.example.test/api/proxy/s/share-token/events', {
      headers: { Accept: 'text/event-stream' },
    })

    const response = await GET(request, {
      params: Promise.resolve({ path: ['s', 'share-token', 'events'] }),
    })
    const upstreamSignal = fetchMock.mock.calls[0][1]?.signal

    await response.body?.cancel('client disconnected')

    expect(upstreamSignal?.aborted).toBe(true)
  })

  it('propagates an incoming request abort to the upstream fetch', async () => {
    vi.stubEnv('AGENTAPI_PROXY_URL', 'http://backend:8080')
    const downstreamAbort = new AbortController()
    let upstreamSignal: AbortSignal | undefined
    vi.spyOn(globalThis, 'fetch').mockImplementation((_url, options) => {
      upstreamSignal = options?.signal as AbortSignal | undefined
      return new Promise<Response>((_resolve, reject) => {
        upstreamSignal?.addEventListener('abort', () => {
          reject(upstreamSignal?.reason ?? new DOMException('Aborted', 'AbortError'))
        }, { once: true })
      })
    })
    const request = new NextRequest('https://ui.example.test/api/proxy/s/share-token/events', {
      headers: { Accept: 'text/event-stream' },
      signal: downstreamAbort.signal,
    })

    const pendingResponse = GET(request, {
      params: Promise.resolve({ path: ['s', 'share-token', 'events'] }),
    })
    await vi.waitFor(() => expect(upstreamSignal).toBeDefined())
    downstreamAbort.abort(new DOMException('Client disconnected', 'AbortError'))

    const response = await pendingResponse
    expect(upstreamSignal?.aborted).toBe(true)
    expect(response.status).toBe(499)
  })
})
