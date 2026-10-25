import type { NextConfig } from "next";
import path from "node:path";

const nextConfig: NextConfig = {
  // The documentation is read from the repository root (../docs and a few
  // top-level markdown files), so tracing must be allowed to look there.
  outputFileTracingRoot: path.join(__dirname, ".."),
  turbopack: { root: path.join(__dirname, "..") },
  poweredByHeader: false,
};

export default nextConfig;
