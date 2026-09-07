"use client";

import type { InputHTMLAttributes } from "react";
import type { DataAttributes } from "@/app/components/buttons/Button";

export interface TextFieldProps extends InputHTMLAttributes<HTMLInputElement>, DataAttributes {
  label: string;
  id?: string;
}

export function TextField({ label, id, className, ...rest }: TextFieldProps) {
  const inputId = id ?? `field-${label.toLowerCase().replace(/[^a-z0-9]+/g, "-")}`;
  return (
    <div className="field">
      <label className="field-label" htmlFor={inputId}>
        {label}
      </label>
      <input id={inputId} className={`field-input${className ? ` ${className}` : ""}`} {...rest} />
    </div>
  );
}
