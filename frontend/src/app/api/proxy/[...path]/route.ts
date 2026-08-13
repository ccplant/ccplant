import { NextRequest, NextResponse } from 'next/server'
import { getApiKeyFromCookie, renewApiKeyCookie } from '@/lib/cookie-auth'
import { getBackendBaseUrl } from '@/lib/server-backend-url'
import {
  buildDownstreamResponseHeaders,
  buildUpstreamRequestHeaders,
  isServerSentEventsRequest,
  isUnauthenticatedProxyPath,
  requestCanHaveBody,
} from '@/lib/proxy-transport'

type RouteContext = { params: Promise<{ path: string[] }> }

export async function GET(request: NextRequest, context: RouteContext) {
  return routeProxyRequest(request, context, 'GET')
}

export async function POST(request: NextRequest, context: RouteContext) {
  return routeProxyRequest(request, context, 'POST')
}

export async function PUT(request: NextRequest, context: RouteContext) {
  return routeProxyRequest(request, context, 'PUT')
}

export async function DELETE(request: NextRequest, context: RouteContext) {
  return routeProxyRequest(request, context, 'DELETE')
}

export async function PATCH(request: NextRequest, context: RouteContext) {
  return routeProxyRequest(request, context, 'PATCH')
}

const DEBUG_ENABLED = process.env.NODE_ENV !== 'production' && process.env.DEBUG_LOGS === 'true'

function debugLog(message: string, metadata?: Record<string, unknown>) {
  if (DEBUG_ENABLED) {
    console.log(message, metadata)
  }
}

async function routeProxyRequest(
  request: NextRequest,
  context: RouteContext,
  method: string,
): Promise<NextResponse> {
  const { path } = await context.params
  return handleProxyRequest(request, path, method)
}

interface LinkedAbort {
  controller: AbortController
  cleanup: () => void
}

function createLinkedAbort(requestSignal: AbortSignal, timeoutMs?: number): LinkedAbort {
  const controller = new AbortController()
  let timeout: ReturnType<typeof setTimeout> | undefined

  const abortFromRequest = () => {
    if (!controller.signal.aborted) {
      controller.abort(requestSignal.reason)
    }
  }

  if (requestSignal.aborted) {
    abortFromRequest()
  } else {
    requestSignal.addEventListener('abort', abortFromRequest, { once: true })
  }

  if (timeoutMs !== undefined) {
    timeout = setTimeout(() => {
      if (!controller.signal.aborted) {
        controller.abort(new DOMException('Upstream request timed out', 'TimeoutError'))
      }
    }, timeoutMs)
  }

  return {
    controller,
    cleanup: () => {
      requestSignal.removeEventListener('abort', abortFromRequest)
      if (timeout !== undefined) clearTimeout(timeout)
    },
  }
}

function proxyUpstreamResponse(response: Response, linkedAbort: LinkedAbort): NextResponse {
  const headers = buildDownstreamResponseHeaders(response.headers)
  const responseInit = {
    status: response.status,
    statusText: response.statusText,
    headers,
  }

  if (!response.body || response.status === 204 || response.status === 205 || response.status === 304) {
    linkedAbort.cleanup()
    return new NextResponse(null, responseInit)
  }

  const reader = response.body.getReader()
  let finished = false
  const finish = () => {
    if (finished) return
    finished = true
    linkedAbort.cleanup()
  }

  const stream = new ReadableStream<Uint8Array>({
    async pull(controller) {
      try {
        const { done, value } = await reader.read()
        if (done) {
          finish()
          controller.close()
          return
        }
        controller.enqueue(value)
      } catch (error) {
        finish()
        controller.error(error)
      }
    },
    async cancel(reason) {
      if (!linkedAbort.controller.signal.aborted) {
        linkedAbort.controller.abort(reason)
      }
      try {
        await reader.cancel(reason)
      } catch {
        // The abort may already have closed the upstream reader.
      } finally {
        finish()
      }
    },
  })

  return new NextResponse(stream, responseInit)
}

async function handleProxyRequest(
  request: NextRequest,
  pathParts: string[],
  method: string,
): Promise<NextResponse> {
  let linkedAbort: LinkedAbort | undefined

  try {
    // Preserve percent-encoded path separators decoded by Next.js catch-all params.
    const proxyPrefix = '/api/proxy/'
    const rawPathname = request.nextUrl.pathname
    const path = rawPathname.startsWith(proxyPrefix)
      ? rawPathname.slice(proxyPrefix.length)
      : pathParts.join('/')

    const authenticationRequired = !isUnauthenticatedProxyPath(path)
    let apiKey: string | null = null
    if (authenticationRequired) {
      apiKey = await getApiKeyFromCookie()
      if (!apiKey) {
        return NextResponse.json(
          {
            error: 'Authentication required',
            message: 'Please log in again.',
            code: 'NO_API_KEY',
          },
          { status: 401 },
        )
      }

      if (path === 'start' && method === 'POST') {
        await renewApiKeyCookie()
      }
    }

    const body = requestCanHaveBody(method) ? await request.arrayBuffer() : undefined
    const headers = buildUpstreamRequestHeaders(request.headers, apiKey)
    const targetUrl = `${getBackendBaseUrl()}/${path}${request.nextUrl.search}`
    const isSSE = isServerSentEventsRequest(headers)
    const isMessageEndpoint = pathParts.includes('message')
      || (pathParts.includes('messages') && pathParts.includes('wait'))
    const timeoutMs = isSSE ? undefined : (isMessageEndpoint ? 120000 : 35000)

    linkedAbort = createLinkedAbort(request.signal, timeoutMs)
    debugLog('[API Proxy] Forwarding request', {
      method,
      path: `/${path}`,
      authenticated: authenticationRequired,
      streaming: isSSE,
    })

    const response = await fetch(targetUrl, {
      method,
      headers,
      body,
      signal: linkedAbort.controller.signal,
      redirect: 'manual',
    })

    debugLog('[API Proxy] Received upstream response', {
      method,
      path: `/${path}`,
      status: response.status,
      contentType: response.headers.get('content-type'),
    })

    const result = proxyUpstreamResponse(response, linkedAbort)
    linkedAbort = undefined
    return result
  } catch (error) {
    linkedAbort?.cleanup()

    if (error instanceof Error && error.name === 'TimeoutError') {
      return NextResponse.json(
        {
          error: 'Request timeout',
          message: 'リクエストがタイムアウトしました。処理に時間がかかっている可能性があります。',
          code: 'TIMEOUT_ERROR',
        },
        { status: 408 },
      )
    }

    if (request.signal.aborted) {
      return NextResponse.json({ error: 'Client closed request' }, { status: 499 })
    }

    console.error('Proxy request failed:', error instanceof Error ? error.name : 'Unknown error')
    return NextResponse.json(
      {
        error: 'Internal proxy error',
        code: 'PROXY_ERROR',
      },
      { status: 502 },
    )
  }
}
