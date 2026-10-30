/** @type {import('next').NextConfig} */
const config = {
  poweredByHeader: false,
  reactStrictMode: true,
  // "/" and the URLs from before pages were nested under auth/ and app/.
  async redirects() {
    return [
      { source: "/", destination: "/app/dashboard", permanent: false },
      { source: "/app", destination: "/app/dashboard", permanent: false },
      { source: "/auth", destination: "/auth/login", permanent: false },
      { source: "/login", destination: "/auth/login", permanent: false },
      { source: "/projects", destination: "/app/projects", permanent: false },
      { source: "/settings", destination: "/app/settings", permanent: false },
    ];
  },
  async rewrites() {
    return [
      {
        source: "/api/:path*",
        destination: `${process.env.API_URL || "http://127.0.0.1:8080"}/api/:path*`,
      },
    ];
  },
  async headers() {
    return [
      {
        source: "/:path*",
        headers: [
          { key: "X-Content-Type-Options", value: "nosniff" },
          { key: "X-Frame-Options", value: "DENY" },
          { key: "Referrer-Policy", value: "strict-origin-when-cross-origin" },
          {
            key: "Permissions-Policy",
            value: "camera=(), microphone=(), geolocation=()",
          },
        ],
      },
    ];
  },
};
export default config;
