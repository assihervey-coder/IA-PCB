"use client";

import type { ButtonHTMLAttributes, ReactNode } from "react";

export type ButtonVariant = "primary" | "secondary" | "danger" | "ghost";
export type ButtonSize = "sm" | "md";

/** Permet de passer des attributs data-* (ex. data-testid) aux composants. */
export interface DataAttributes {
  [key: `data-${string}`]: string | undefined;
}

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement>, DataAttributes {
  variant?: ButtonVariant;
  size?: ButtonSize;
  children: ReactNode;
}

export function Button({
  variant = "primary",
  size = "md",
  className,
  children,
  type = "button",
  ...rest
}: ButtonProps) {
  return (
    <button
      type={type}
      className={`btn btn-${variant} btn-${size}${className ? ` ${className}` : ""}`}
      {...rest}
    >
      {children}
    </button>
  );
}
