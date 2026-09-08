"use client";

import type { ButtonHTMLAttributes, ReactNode } from "react";
import type { DataAttributes } from "./Button";

export interface IconButtonProps extends ButtonHTMLAttributes<HTMLButtonElement>, DataAttributes {
  /** Libellé accessible (aria-label + title). */
  label: string;
  children: ReactNode;
}

export function IconButton({ label, className, children, type = "button", ...rest }: IconButtonProps) {
  return (
    <button
      type={type}
      className={`icon-btn${className ? ` ${className}` : ""}`}
      aria-label={label}
      title={label}
      {...rest}
    >
      {children}
    </button>
  );
}
