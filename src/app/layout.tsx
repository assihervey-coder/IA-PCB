import type { Metadata } from "next";
import { Geist, Geist_Mono } from "next/font/google";
import "./globals.css";
import { Toaster } from "@/components/ui/toaster";

const geistSans = Geist({
  variable: "--font-geist-sans",
  subsets: ["latin"],
});

const geistMono = Geist_Mono({
  variable: "--font-geist-mono",
  subsets: ["latin"],
});

export const metadata: Metadata = {
  title: "YahriaCad-Pro IA — CAO électronique avec placement & routage IA",
  description:
    "Éditeur de schémas, éditeur de PCB, placement par recuit simulé, routage A* multi-couches, DRC/ERC et export Gerber/STEP/BOM.",
  keywords: ["PCB", "CAO", "EDA", "Gerber", "routage automatique", "IA", "KiCad"],
  authors: [{ name: "YahriaCad Contributors" }],
  icons: {
    icon: "https://z-cdn.chatglm.cn/z-ai/static/logo.svg",
  },
  openGraph: {
    title: "YahriaCad-Pro IA",
    description: "CAO électronique avec placement & routage IA",
    siteName: "YahriaCad",
    type: "website",
  },
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="fr" suppressHydrationWarning>
      <body
        className={`${geistSans.variable} ${geistMono.variable} antialiased bg-background text-foreground`}
      >
        {children}
        <Toaster />
      </body>
    </html>
  );
}
