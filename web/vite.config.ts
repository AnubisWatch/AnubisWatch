import path from "node:path";
import tailwindcss, { type Config } from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

const config: Config = {
	darkMode: "class",
};

export default defineConfig({
	plugins: [react(), tailwindcss(config)],
	resolve: {
		alias: {
			"@": path.resolve(__dirname, "./src"),
		},
	},
	build: {
		outDir: "dist",
		emptyOutDir: true,
		rolldownOptions: {
			output: {
				codeSplitting: {
					groups: [
						{
							name: "react-core",
							test: /node_modules\/(react|react-dom)\//,
						},
						{
							name: "router-state",
							test: /node_modules\/(react-router|react-router-dom|zustand)\//,
						},
						{
							name: "charts",
							test: /node_modules\/recharts\//,
						},
						{
							name: "icons",
							test: /node_modules\/lucide-react\//,
						},
					],
				},
			},
		},
	},
	server: {
		port: 3000,
		proxy: {
			"/api": {
				target: "http://localhost:8080",
				changeOrigin: true,
			},
			"/ws": {
				target: "ws://localhost:8080",
				ws: true,
			},
		},
	},
});
