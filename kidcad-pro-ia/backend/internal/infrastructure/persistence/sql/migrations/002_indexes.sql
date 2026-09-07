-- KidCAD-Pro-IA — index de requêtage (listes paginées, filtres par statut)

CREATE INDEX IF NOT EXISTS idx_projects_status ON projects (status);
CREATE INDEX IF NOT EXISTS idx_projects_created_at ON projects (created_at);
