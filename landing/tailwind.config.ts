import type { Config } from "tailwindcss";

const config: Config = {
  content: ["./app/**/*.{ts,tsx}", "./components/**/*.{ts,tsx}", "./lib/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        ink: "#111827",
        muted: "#6b7280",
        line: "#d9dee8",
        panel: "#ffffff",
        shell: "#f5f7fb",
        brand: "#0f766e",
        accent: "#b91c1c",
        coral: "#f9736b"
      },
      boxShadow: {
        soft: "0 12px 30px rgba(17, 24, 39, 0.08)"
      }
    }
  },
  plugins: []
};

export default config;
