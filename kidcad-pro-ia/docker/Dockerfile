# ==============================================================
# KidCAD-Pro-IA — image Docker du backend Go
#
# Construction (depuis la racine du monorepo) :
#   docker build -f docker/Dockerfile -t kidcad-backend .
#
# L'image finale est distroless/static : pas de shell, pas de gestionnaire
# de paquets, surface d'attaque minimale. Le binaire est compilé avec
# CGO_ENABLED=0 (adaptateur de persistance mémoire / pgx pur Go).
# ==============================================================

# ---- Étape 1 : compilation ----------------------------------------------
FROM golang:1.22-alpine AS builder

WORKDIR /src

# Cache de modules : seuls go.mod / go.sum invalident cette couche.
COPY go.mod go.sum ./
RUN go mod download

# Sources : backend (cmd + internal + pkg) et stubs gRPC générés (shared/gen/go).
COPY backend/ ./backend/
COPY shared/gen/go/ ./shared/gen/go/

RUN CGO_ENABLED=0 GOOS=linux go build \
        -ldflags "-s -w" \
        -o /out/kidcad-server \
        ./backend/cmd/kidcad-server

# ---- Étape 2 : image d'exécution ----------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /out/kidcad-server /kidcad-server

# Variables d'environnement par défaut (surchargées par docker-compose / env) :
#   KIDCAD_HTTP_PORT=8080, KIDCAD_DB_URL="", KIDCAD_AI_ADDR=ai-engine:50051
EXPOSE 8080

USER nonroot:nonroot

ENTRYPOINT ["/kidcad-server"]
