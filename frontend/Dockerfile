# Build Next.js on the build host architecture. The generated standalone output
# is architecture-independent, so cross-platform images only need an
# architecture-specific Bun runtime in the final stage.
FROM --platform=$BUILDPLATFORM oven/bun:1.3.5-alpine AS base

# Set working directory
WORKDIR /app

# Copy package files
COPY package.json bun.lockb* ./

# Build stage
FROM base AS builder
COPY . .
RUN bun install --frozen-lockfile
RUN bun run build

# Production stage
FROM oven/bun:1.3.5-alpine AS runner
WORKDIR /app

# Create non-root user
RUN addgroup --system --gid 1001 nodejs
RUN adduser --system --uid 1001 nextjs

# Copy built application
COPY --from=builder /app/public ./public
COPY --from=builder --chown=nextjs:nodejs /app/.next/standalone ./
COPY --from=builder --chown=nextjs:nodejs /app/.next/static ./.next/static

# No need for runtime config generation - using API Routes instead

USER nextjs

EXPOSE 3000

ENV PORT=3000
ENV HOSTNAME="0.0.0.0"
ARG VALIDATE_API_KEY_WITH_PROXY=true
ENV VALIDATE_API_KEY_WITH_PROXY=${VALIDATE_API_KEY_WITH_PROXY}

CMD ["bun", "server.js"]
