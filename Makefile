# =============================================================================
# KidCAD-Pro-IA — Makefile
# Aide : make help
# =============================================================================

.PHONY: help install db-push dev ai-service dev-all lint test docker clean

# --- Défaut : aide ------------------------------------------------------------
help: ## Affiche cette aide
	@echo "KidCAD-Pro-IA — cibles disponibles :"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

install: ## Installe les dépendances Bun (racine + mini-service IA)
	bun install
	cd mini-services/ai-engine && bun install

db-push: ## Applique le schéma Prisma à SQLite (db/custom.db)
	bun run db:push

dev: ## Lance l'application Next.js en développement (port 3000)
	bun run dev

ai-service: ## Lance le mini-service IA socket.io (port 3010)
	cd mini-services/ai-engine && bun run dev

dev-all: ## Lancement complet (note : 2 terminaux requis)
	@echo "Le lancement complet nécessite 2 terminaux (pas de job control ici) :"
	@echo ""
	@echo "  Terminal 1 :  make dev          (Next.js, http://localhost:3000)"
	@echo "  Terminal 2 :  make ai-service   (service IA socket.io, :3010)"
	@echo ""
	@echo "Alternative conteneurs : make docker"

lint: ## Lint ESLint
	bun run lint

test: ## Lance les tests (bun test tests/)
	bun test tests/

docker: ## Construit et lance les conteneurs (app + ai-engine)
	docker compose -f docker/docker-compose.yml up --build

clean: ## Nettoie les artefacts de build et caches locaux
	rm -rf .next .turbo node_modules/.cache
	rm -rf mini-services/ai-engine/node_modules/.cache
	@echo "Nettoyage terminé (node_modules et db/custom.db conservés)."
