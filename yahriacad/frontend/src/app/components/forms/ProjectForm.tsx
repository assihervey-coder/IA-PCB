"use client";

import { useState } from "react";
import type { Project, ProjectCreate } from "@/lib/api/types";
import { Button } from "@/app/components/buttons/Button";
import { TextField } from "./TextField";
import { SelectField } from "./SelectField";

export interface ProjectFormProps {
  /** Projet existant pour l'édition, sinon création. */
  initial?: Project | null;
  submitting?: boolean;
  onSubmit: (data: ProjectCreate) => void;
  onCancel: () => void;
}

const LAYER_OPTIONS = [
  { value: "1", label: "1 couche" },
  { value: "2", label: "2 couches" },
  { value: "4", label: "4 couches" },
  { value: "6", label: "6 couches" },
];

export function ProjectForm({ initial, submitting = false, onSubmit, onCancel }: ProjectFormProps) {
  const [name, setName] = useState(initial?.name ?? "");
  const [description, setDescription] = useState(initial?.description ?? "");
  const [layerCount, setLayerCount] = useState(String(initial?.layer_count ?? 2));

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const trimmed = name.trim();
    if (trimmed.length === 0) return;
    onSubmit({
      name: trimmed,
      description: description.trim(),
      layer_count: Number(layerCount) || 2,
    });
  };

  return (
    <form onSubmit={handleSubmit}>
      <TextField
        label="Nom du projet"
        data-testid="project-form-name"
        value={name}
        onChange={(e) => setName(e.target.value)}
        placeholder="ex. Robot suiveur de ligne"
        required
        maxLength={80}
        autoFocus
      />
      <TextField
        label="Description"
        data-testid="project-form-description"
        value={description}
        onChange={(e) => setDescription(e.target.value)}
        placeholder="Objectif de la carte, contraintes, remarques…"
        maxLength={240}
      />
      <SelectField
        label="Nombre de couches"
        data-testid="project-form-layers"
        options={LAYER_OPTIONS}
        value={layerCount}
        onChange={(e) => setLayerCount(e.target.value)}
      />
      <div className="form-actions">
        <Button variant="ghost" onClick={onCancel}>
          Annuler
        </Button>
        <Button type="submit" data-testid="project-form-submit" disabled={submitting || name.trim().length === 0}>
          {submitting ? "Création…" : initial ? "Enregistrer" : "Créer le projet"}
        </Button>
      </div>
    </form>
  );
}
