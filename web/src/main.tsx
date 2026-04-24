import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import './index.css'
import App from './App'
import { useThemeStore, applyTheme } from './stores/themeStore'

// Apply saved theme on startup
const savedTheme = useThemeStore.getState().theme
applyTheme(savedTheme)

// Register service worker for PWA support
if ('serviceWorker' in navigator) {
  window.addEventListener('load', () => {
    navigator.serviceWorker.register('/sw.js').then((registration) => {
      // Listen for updates
      registration.addEventListener('updatefound', () => {
        const newWorker = registration.installing;
        if (newWorker) {
          newWorker.addEventListener('statechange', () => {
            if (newWorker.state === 'installed' && navigator.serviceWorker.controller) {
              // New service worker available, prompt user to refresh
              if (confirm('AnubisWatch has been updated. Reload to use the latest version?')) {
                window.location.reload();
              }
            }
          });
        }
      });
    }).catch(() => {
      // Service worker registration failed — likely running in dev mode
    });
  });
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <BrowserRouter>
      <App />
    </BrowserRouter>
  </StrictMode>,
)
