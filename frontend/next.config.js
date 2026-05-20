/** @type {import('next').NextConfig} */
const nextConfig = {
  output: "standalone",
  reactStrictMode: true,
  // Public env consumed by client components. INDEXER_URL is what the browser hits;
  // CHAIN_URL is the aios chain REST/SSE endpoint.
  env: {
    NEXT_PUBLIC_INDEXER_URL: process.env.NEXT_PUBLIC_INDEXER_URL || "http://localhost:8081",
    NEXT_PUBLIC_CHAIN_URL: process.env.NEXT_PUBLIC_CHAIN_URL || "http://localhost:26657",
  },
};

module.exports = nextConfig;
