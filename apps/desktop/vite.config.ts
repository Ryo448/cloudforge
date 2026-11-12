import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
export default defineConfig({
  plugins: [
    react(),
    tailwindcss(),
    {
      name: "cloudforge-dev-csp",
      apply: "serve",
      transformIndexHtml(html) {
        return html.replace(
          "script-src 'self';",
          "script-src 'self' 'unsafe-inline';",
        );
      },
    },
  ],
  base: "./",
  build: { outDir: "dist", emptyOutDir: true },
  server: { host: "127.0.0.1", strictPort: true },
});
