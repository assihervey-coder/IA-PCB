import type { Metadata } from "next";
import { AppShell } from "@/app/layout/AppShell";
import "@/styles/globals.scss";

export const metadata: Metadata = {
  title: "KidCAD-Pro-IA",
  description:
    "Éditeur PCB assisté par IA : schéma, placement et routage automatiques, vérifications DRC/ERC, visualisation 3D et exports Gerber.",
  icons: {
    icon: "/favicon.svg",
  },
};

export default function RootLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="fr">
      <body>
        <AppShell>{children}</AppShell>
      </body>
    </html>
  );
}
