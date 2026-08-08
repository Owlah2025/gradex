/** @type {import('next').NextConfig} */
const nextConfig = {
  output: "standalone",
  reactStrictMode: true,
  poweredByHeader: false,
  async rewrites() {
    if (process.env.NODE_ENV !== "development") return [];
    const apiOrigin = (process.env.GRADEX_API_ORIGIN ?? "http://127.0.0.1:8080").replace(/\/$/, "");
    return [
      {
        source: "/api/:path*",
        destination: `${apiOrigin}/api/:path*`,
      },
    ];
  },
};

export default nextConfig;
