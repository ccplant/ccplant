import { createProxyRouteHandler } from '@/lib/server-proxy-route'
import type { NextRequest } from 'next/server'

const options = {
  publicPrefix: '/mcp',
  upstreamPath: 'mcp',
  passThroughAuthorization: true,
  // Streamable HTTP clients advertise both JSON and SSE. Wait for the
  // backend response headers so its selected representation stays intact.
  eagerSSE: false,
}

function createMCPRouteHandler(method: string) {
  const proxy = createProxyRouteHandler(method, options)

  // OpenNext does not provide dynamic params context for a static route.
  // Supply the empty path explicitly instead of depending on adapter context.
  return (request: NextRequest) => proxy(request, { params: Promise.resolve({}) })
}

export const GET = createMCPRouteHandler('GET')
export const POST = createMCPRouteHandler('POST')
export const DELETE = createMCPRouteHandler('DELETE')
