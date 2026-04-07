# AnubisWatch - Project Status

## ✅ Tamamlanan İşlemler

### Backend (Go)
- [x] REST API server (port 8080/3000)
- [x] WebSocket server (port 8080)
- [x] SSE (Server-Sent Events) endpoint
- [x] MCP (Model Context Protocol) desteği
- [x] JWT Authentication
- [x] Rate limiting (100 requests/minute)
- [x] CORS middleware
- [x] CobaltDB storage
- [x] ACME/Let's Encrypt desteği

### API Endpoints
- [x] `/health`, `/ready` - Health checks
- [x] `/api/v1/auth/*` - Authentication (login/logout/me)
- [x] `/api/v1/souls/*` - CRUD + force check + judgments
- [x] `/api/v1/judgments/*` - List/Get judgments
- [x] `/api/v1/channels/*` - Alert channels CRUD + test
- [x] `/api/v1/rules/*` - Alert rules CRUD
- [x] `/api/v1/workspaces/*` - Workspaces CRUD
- [x] `/api/v1/stats/*` - Stats & overview
- [x] `/api/v1/cluster/*` - Cluster/Raft status
- [x] `/api/v1/status-pages/*` - Status pages CRUD
- [x] `/api/v1/events` - SSE endpoint
- [x] `/ws` - WebSocket endpoint
- [x] `/api/v1/mcp` - MCP endpoint

### Frontend (React + TypeScript + Vite)
- [x] Modern UI tasarımı (gradient, glow efektleri)
- [x] Dashboard sayfası (gerçek API entegrasyonu)
- [x] Souls sayfası (list/grid görünüm)
- [x] SoulDetail sayfası (detaylı görünüm)
- [x] Judgments sayfası (historik veriler)
- [x] Alerts sayfası (kanallar & kurallar)
- [x] Journeys sayfası (multi-step workflows)
- [x] Cluster sayfası (node monitoring)
- [x] StatusPages sayfası (public status pages)
- [x] Settings sayfası (yapılandırma)
- [x] Sidebar navigasyonu
- [x] Header bileşeni

### API Client & Hooks
- [x] `api/client.ts` - API client sınıfı
- [x] `api/hooks.ts` - React hooks (useSouls, useStats, vb.)
- [x] Auth entegrasyonu (localStorage token)
- [x] Error handling

### State Management
- [x] Zustand store'ları
- [x] Souls store
- [x] Judgments store

## 📁 Proje Yapısı

```
AnubisWatch/
├── cmd/anubis/           # Main entry point
│   └── main.go
├── internal/
│   ├── api/              # REST API
│   ├── auth/             # Authentication
│   ├── cluster/          # Raft cluster
│   ├── core/             # Domain models
│   ├── dashboard/        # Static file serving
│   ├── probe/            # Health check probes
│   ├── storage/          # CobaltDB
│   └── ...
├── web/                  # React frontend
│   ├── src/
│   │   ├── api/          # API client & hooks
│   │   ├── components/   # Layout, Sidebar, Header
│   │   ├── pages/        # All page components
│   │   └── stores/       # Zustand stores
│   └── dist/             # Build output
├── anubis.json           # Config file
└── data/                 # Database files
```

## 🚀 Çalıştırma

```bash
# Config ile birlikte çalıştırma
ANUBIS_CONFIG=./anubis.json ANUBIS_PORT=3000 go run ./cmd/anubis serve

# Frontend build
cd web && npm run build
```

## 📡 API Örnekleri

```bash
# Stats overview
curl http://localhost:3000/api/v1/stats/overview

# List souls
curl http://localhost:3000/api/v1/souls

# Cluster status
curl http://localhost:3000/api/v1/cluster/status
```

## 🔧 Mevcut Durum

- Server: ✅ Çalışıyor (http://localhost:3000)
- Frontend: ✅ Build başarılı
- API: ✅ Tüm endpoint'ler aktif
- WebSocket: ✅ Aktif
- Auth: ✅ JWT ile çalışıyor (dev modda otomatik anonymous)

## 📝 Notlar

- Auth devre dışı bırakıldığında otomatik anonymous kullanıcı atanır
- WebSocket kullanılamazsa SSE otomatik fallback olarak çalışır
- Tüm sayfalar responsive tasarıma sahip
- Renk paleti: Amber (primary), Emerald (success), Rose (error), Blue (info)
