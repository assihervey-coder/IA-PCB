# ==============================================================
# KidCAD-Pro-IA — Makefile
# Commandes de build, de dev et de test pour les 3 modules :
#   backend (Go) | ai-engine (Python/gRPC) | frontend (Next.js)
# ==============================================================

SHELL         := /bin/bash
.DEFAULT_GOAL := help

GO        ?= go
PYTHON    ?= python3
ROOT_DIR  := $(CURDIR)
BIN_DIR   := $(ROOT_DIR)/bin
BACKEND_MAIN := ./backend/cmd/kidcad-server
PROTO        := shared/types/pcb.proto
PROTO_GEN_PY := shared/gen/python
PROTO_GEN_GO := shared/gen/go

VERSION ?= 0.1.0
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: help deps proto build build-backend build-frontend \
	run-backend run-ai run-frontend \
	test test-backend test-ai test-frontend lint models \
	docker-build docker-up docker-down clean

help: ## Affiche cette aide
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
                | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

deps: ## Installe toutes les dépendances (Go, Python, Node)
	$(GO) mod download
	$(PYTHON) -m pip install -r requirements.txt
	cd frontend && npm install
	@echo "Dépendances installées."

proto: ## Génère les stubs gRPC (Python + Go si les plugins sont présents)
	@mkdir -p $(PROTO_GEN_PY) $(PROTO_GEN_GO)
	$(PYTHON) -m grpc_tools.protoc -I shared/types \
                --python_out=$(PROTO_GEN_PY) --grpc_python_out=$(PROTO_GEN_PY) --pyi_out=$(PROTO_GEN_PY) $(PROTO) \
                || echo "grpcio-tools manquant : pip install grpcio-tools"
	@if command -v protoc-gen-go >/dev/null 2>&1 && command -v protoc-gen-go-grpc >/dev/null 2>&1; then \
                python3 -m grpc_tools.protoc -I shared/types \
                        --plugin=protoc-gen-go=$$(command -v protoc-gen-go) \
                        --plugin=protoc-gen-go-grpc=$$(command -v protoc-gen-go-grpc) \
                        --go_out=module=github.com/kidcad/kidcad-pro-ia:$(ROOT_DIR) \
                        --go-grpc_out=module=github.com/kidcad/kidcad-pro-ia:$(ROOT_DIR) $(PROTO); \
                echo "Stubs Go générés."; \
	else \
                echo "protoc-gen-go / protoc-gen-go-grpc absents : go install google.golang.org/protobuf/cmd/protoc-gen-go google.golang.org/grpc/cmd/protoc-gen-go-grpc"; \
	fi

build: build-backend build-frontend ## Compile le backend et le frontend

build-backend: ## Compile le serveur Go -> bin/kidcad-server
	@mkdir -p $(BIN_DIR)
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/kidcad-server $(BACKEND_MAIN)
	@echo "Backend compilé : bin/kidcad-server"

build-frontend: ## Compile le frontend Next.js (frontend/.next)
	cd frontend && npm run build

run-backend: ## Lance le serveur backend Go (http://localhost:8080)
	$(GO) run $(BACKEND_MAIN)

run-ai: ## Lance le moteur IA gRPC (port 50051)
	cd ai-engine && $(PYTHON) cmd/ai-server/main.py

run-frontend: ## Lance le frontend Next.js (http://localhost:3000)
	cd frontend && npm run dev

test: test-backend test-ai ## Lance tous les tests backend + IA

test-backend: ## Tests Go (unitaires + intégration)
	$(GO) test ./... 

test-ai: ## Tests Python (pytest sur ai-engine)
	cd ai-engine && $(PYTHON) -m pytest -q

test-frontend: ## Tests E2E Playwright (tests/e2e)
	npm --prefix tests/e2e run test

lint: ## Vérifications statiques (gofmt, ruff, tsc)
	gofmt -l ./backend ./shared/gen 2>/dev/null || true
	cd ai-engine && $(PYTHON) -m ruff check src cmd evaluation 2>/dev/null || true
	cd frontend && npx tsc --noEmit 2>/dev/null || true

models: ## Génère les checkpoints .pt initiaux (nécessite torch : pip install torch)
	bash scripts/generate-models.sh

train-router: ## Entraîne le routeur PPO (torch requis) : make train-router STEPS=200000
	python3 ai-engine/training/router/train.py --steps $(or $(STEPS),200000)

docker-build: ## Construit les images Docker (backend, frontend)
	docker compose -f docker/docker-compose.yml build

docker-up: ## Démarre la stack complète (postgres, backend, ai-engine, frontend)
	docker compose -f docker/docker-compose.yml up -d

docker-down: ## Arrête la stack Docker
	docker compose -f docker/docker-compose.yml down

clean: ## Nettoie les artefacts de build
	rm -rf $(BIN_DIR) frontend/.next frontend/node_modules/.cache
	find . -type d -name "__pycache__" -prune -exec rm -rf {} \; 2>/dev/null || true
