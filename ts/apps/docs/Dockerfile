# syntax=docker/dockerfile:1

# ---- deps stage ----
FROM node:22-alpine AS deps
RUN corepack enable
WORKDIR /app
COPY package.json pnpm-workspace.yaml pnpm-lock.yaml ./
COPY apps/docs/package.json ./apps/docs/package.json
COPY packages/typescript-config/package.json ./packages/typescript-config/package.json
COPY packages/ui/package.json ./packages/ui/package.json
RUN pnpm install --frozen-lockfile

# ---- builder stage ----
FROM node:22-alpine AS builder
RUN corepack enable
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY --from=deps /app/apps/docs/node_modules ./apps/docs/node_modules
COPY ./ ./
WORKDIR /app/apps/docs

RUN pnpm run build

# ---- runtime stage ----
FROM node:22-alpine AS runner
WORKDIR /app

ENV NODE_ENV=production

RUN addgroup -g 10001 -S nodejs \
 && adduser -S nextjs -u 10001 -G nodejs

# Next.js standalone output mirrors the monorepo layout, so server.js lives at
# .next/standalone/apps/docs/server.js and resolves _next/static relative to its
# own directory. Copy the app subtree so server.js, .next/static, and public sit
# next to each other where the server expects them.
COPY --from=builder --chown=nextjs:nodejs /app/apps/docs/.next/standalone ./
COPY --from=builder --chown=nextjs:nodejs /app/apps/docs/.next/static ./apps/docs/.next/static
COPY --from=builder --chown=nextjs:nodejs /app/apps/docs/public ./apps/docs/public
# openapi/openapi.json is re-read at request time (the standalone server
# chdirs to its own directory, and the spec path is resolved relative to it),
# so it must ship with the image. Without it, the sitemap and search routes
# fail at runtime even though they prerender fine at build time.
COPY --from=builder --chown=nextjs:nodejs /app/apps/docs/openapi ./apps/docs/openapi

USER nextjs
EXPOSE 3000

ENV PORT=3000
ENV HOSTNAME="0.0.0.0"

CMD ["node", "apps/docs/server.js"]
