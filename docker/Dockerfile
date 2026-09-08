# syntax=docker/dockerfile:1
# =============================================================================
# YahriaCad — Image backend + frontend (Next.js 16, monolithe modulaire)
# Build : docker build -f docker/Dockerfile -t yahriacad-app .
# Run   : docker run -p 3000:3000 -e DATABASE_URL="file:../db/custom.db" yahriacad-app
# =============================================================================

# --- Base : Bun 1 (runtime + gestionnaire de paquets du projet) --------------
FROM oven/bun:1 AS base
WORKDIR /app

# --- deps : installation des dépendances (couche re-calée sur bun.lock) ------
FROM base AS deps
COPY package.json bun.lock ./
COPY prisma ./prisma
RUN bun install --frozen-lockfile

# --- builder : génération Prisma + build Next.js (output standalone) ---------
FROM base AS builder
COPY --from=deps /app/node_modules ./node_modules
COPY . .
# Génère le client Prisma (SQLite)
RUN bunx prisma generate
# Build Next.js (output: "standalone" déjà présent dans next.config.ts)
# DATABASE_URL factice : la build ne touche pas la base, seule la présence compte.
ENV DATABASE_URL="file:../db/custom.db"
ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1
RUN bun run build

# --- runner : image finale minimale (serveur standalone) ---------------------
FROM oven/bun:1-slim AS runner
WORKDIR /app
ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1
ENV PORT=3000
ENV HOSTNAME=0.0.0.0
# DATABASE_URL relatif au dossier db/ monté en volume (voir docker-compose.yml)
ENV DATABASE_URL="file:../db/custom.db"

# Utilisateur non privilégié
RUN groupadd --system --gid 1001 nodejs \
    && useradd --system --uid 1001 --gid nodejs nextjs

# Artefact standalone + assets statiques + public (reproduit le script "build")
COPY --from=builder --chown=nextjs:nodejs /app/.next/standalone ./
COPY --from=builder --chown=nextjs:nodejs /app/.next/static ./.next/static
COPY --from=builder --chown=nextjs:nodejs /app/public ./public
# Schéma Prisma + client généré (utile si db:push est lancé au démarrage)
COPY --from=builder --chown=nextjs:nodejs /app/prisma ./prisma

RUN mkdir -p /app/db && chown -R nextjs:nodejs /app/db

USER nextjs
EXPOSE 3000

# Healthcheck sur l'endpoint de santé de l'API
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD bun -e "fetch('http://127.0.0.1:3000/api/health').then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))"

CMD ["bun", "server.js"]
