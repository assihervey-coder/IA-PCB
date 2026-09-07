-- KidCAD-Pro-IA — schéma initial (PostgreSQL)
-- La couche applicative fournit des identifiants UUID v4 (utils.NewID) ;
-- les documents riches (schéma, layout, contraintes) sont stockés en JSONB
-- au format interchange (voir backend/pkg/pcb-format).

CREATE TABLE IF NOT EXISTS projects (
    id           uuid PRIMARY KEY,
    name         text        NOT NULL,
    description  text        NOT NULL DEFAULT '',
    layer_count  integer     NOT NULL DEFAULT 2,
    status       text        NOT NULL DEFAULT 'active',
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    schematic    jsonb,
    layout       jsonb,
    "constraints" jsonb
);
