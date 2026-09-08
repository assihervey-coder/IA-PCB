"use client";

import { useState } from "react";
import type { LayoutComponent } from "@/lib/api/types";
import { Button } from "@/app/components/buttons/Button";
import { TextField } from "./TextField";

export interface ComponentPropertiesPatch {
  value: string;
  footprint: string;
  x: number;
  y: number;
  rotation: number;
  fixed: boolean;
}

export interface ComponentPropertiesFormProps {
  component: LayoutComponent;
  /** Valeur affichée (champ libre, non persisté dans LayoutData). */
  value?: string;
  onSubmit: (patch: ComponentPropertiesPatch) => void;
  onCancel: () => void;
}

function parseNumber(raw: string, fallback: number): number {
  const parsed = Number(raw.replace(",", "."));
  return Number.isFinite(parsed) ? parsed : fallback;
}

export function ComponentPropertiesForm({
  component,
  value = "",
  onSubmit,
  onCancel,
}: ComponentPropertiesFormProps) {
  const [val, setVal] = useState(value);
  const [footprint, setFootprint] = useState(component.footprint);
  const [x, setX] = useState(String(component.x));
  const [y, setY] = useState(String(component.y));
  const [rotation, setRotation] = useState(String(component.rotation));
  const [fixed, setFixed] = useState(component.fixed);

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    onSubmit({
      value: val.trim(),
      footprint: footprint.trim() || component.footprint,
      x: parseNumber(x, component.x),
      y: parseNumber(y, component.y),
      rotation: parseNumber(rotation, component.rotation),
      fixed,
    });
  };

  return (
    <form onSubmit={handleSubmit}>
      <TextField
        label="Valeur"
        data-testid="component-form-value"
        value={val}
        onChange={(e) => setVal(e.target.value)}
        placeholder="ex. 10k, 100nF, NE555…"
      />
      <TextField
        label="Empreinte"
        data-testid="component-form-footprint"
        value={footprint}
        onChange={(e) => setFootprint(e.target.value)}
        placeholder="ex. SOIC-8, R_0603…"
      />
      <div className="form-grid">
        <TextField
          label="X (mm)"
          data-testid="component-form-x"
          type="number"
          step="0.1"
          value={x}
          onChange={(e) => setX(e.target.value)}
        />
        <TextField
          label="Y (mm)"
          data-testid="component-form-y"
          type="number"
          step="0.1"
          value={y}
          onChange={(e) => setY(e.target.value)}
        />
      </div>
      <div className="form-grid">
        <TextField
          label="Rotation (deg)"
          data-testid="component-form-rotation"
          type="number"
          step="15"
          value={rotation}
          onChange={(e) => setRotation(e.target.value)}
        />
        <div style={{ display: "flex", alignItems: "flex-end", paddingBottom: 14 }}>
          <label className="field-checkbox" style={{ marginBottom: 0 }}>
            <input
              type="checkbox"
              data-testid="component-form-fixed"
              checked={fixed}
              onChange={(e) => setFixed(e.target.checked)}
            />
            Position verrouillée
          </label>
        </div>
      </div>
      <div className="form-actions">
        <Button variant="ghost" onClick={onCancel}>
          Annuler
        </Button>
        <Button type="submit" data-testid="component-form-submit">
          Appliquer
        </Button>
      </div>
    </form>
  );
}
