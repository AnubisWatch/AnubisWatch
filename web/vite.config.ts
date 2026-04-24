import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss, { type Config } from '@tailwindcss/vite'
import path from 'path'

const config: Config = {
  darkMode: 'class',
}

export default defineConfig({
  plugins: [react(), tailwindcss(config)],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    rollupOptions: {
      output: {
        manualChunks: {
          vendor: ['react', 'react-dom', 'react-router-dom'],
          charts: ['recharts'],
          icons: ['lucide-react'],
        },
      },
    },
  },
  server: {
    port: 3000,
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/ws': {
        target: 'ws://localhost:8080',
        ws: true,
      },
    },
  },
})
