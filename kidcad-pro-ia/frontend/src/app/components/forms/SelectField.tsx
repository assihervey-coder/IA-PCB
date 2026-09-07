"use client";

import type { SelectHTMLAttributes } from "react";
import type { DataAttributes } from "@/app/components/buttons/Button";

export interface SelectOption {
  value: string;
  label: string;
}

export interface SelectFieldProps extends SelectHTMLAttributes<HTMLSelectElement>, DataAttributes {
  label: string;
  options: SelectOption[];
  id?: string;
}

export function SelectField({ label, options, id, className, ...rest }: SelectFieldProps) {
  const selectId = id ?? `field-${label.toLowerCase().replace(/[^a-z0-9]+/g, "-")}`;
  return (
    <div className="field">
      <label className="field-label" htmlFor={selectId}>
        {label}
      </label>
      <select id={selectId} className={`field-select${className ? ` ${className}` : ""}`} {...rest}>
        {options.map((opt) => (
          <option key={opt.value} value={opt.value}>
            {opt.label}
          </option>
        ))}
      </select>
    </div>
  );
}
