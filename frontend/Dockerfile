# Use the official Bun image as base
FROM oven/bun:1.3.5-alpine AS base

# Set working directory
WORKDIR /app

# Copy package files
COPY package.json bun.lock ./

# Install dependencies
FROM base AS deps
RUN bun install --frozen-lockfile --production

# Build stage
FROM base AS builder
COPY . .
RUN bun install --frozen-lockfile
# Next.js may exit before its worker processes finish when the Bun-provided
# `node` shim is used under BuildKit. Use the Alpine Node runtime for the build
# so `.next/standalone` and `.next/static` are always complete.
RUN apk add --no-cache nodejs \
    && BROWSERSLIST_IGNORE_OLD_DATA=1 NEXT_TELEMETRY_DISABLED=1 \
       /usr/bin/node ./node_modules/next/dist/bin/next build --webpack

# Production stage. Next.js standalone output is a Node.js application. Running
# it with Bun breaks the Next.js 16 middleware bundle at runtime.
FROM node:22-alpine AS runner
WORKDIR /app

# Create non-root user
RUN addgroup --system --gid 1001 nextjs \
    && adduser --system --uid 1001 --ingroup nextjs nextjs

# Copy built application
COPY --from=builder /app/public ./public
COPY --from=builder --chown=nextjs:nextjs /app/.next/standalone ./
COPY --from=builder --chown=nextjs:nextjs /app/.next/static ./.next/static

# No need for runtime config generation - using API Routes instead

USER nextjs

EXPOSE 3000

ENV PORT=3000
ENV HOSTNAME="0.0.0.0"
ARG VALIDATE_API_KEY_WITH_PROXY=true
ENV VALIDATE_API_KEY_WITH_PROXY=${VALIDATE_API_KEY_WITH_PROXY}

CMD ["node", "server.js"]
