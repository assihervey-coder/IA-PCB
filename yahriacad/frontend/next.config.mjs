/** @type {import('next').NextConfig} */

// Origine du backend Go. BACKEND_ORIGIN (ex. http://127.0.0.1:8080) est prioritaire
// sur la valeur par défaut http://localhost:8080.
const backendOrigin = process.env.BACKEND_ORIGIN ?? "http://localhost:8080";

const nextConfig = {
  reactStrictMode: true,
  eslint: {
    ignoreDuringBuilds: true,
  },
  async rewrites() {
    return [
      {
        source: "/api/v1/:path*",
        destination: `${backendOrigin}/api/v1/:path*`,
      },
    ];
  },
};

export default nextConfig;
