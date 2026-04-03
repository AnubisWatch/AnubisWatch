# AnubisWatch — BRANDING.md

> **Brand Identity & Visual Guide**
> **Version:** 1.0.0 · **Date:** 2026-03-30

---

## 1. BRAND IDENTITY

### 1.1 Name

**AnubisWatch**

- Full name: AnubisWatch
- CLI binary: `anubis`
- Casual reference: "Anubis"
- Never: "Anubis Watch" (two words), "anubis-watch" (hyphenated)
- Domain: `anubis.watch` (primary), `anubiswatch.com` (redirect)
- GitHub: `github.com/AnubisWatch`

### 1.2 Taglines

| Context | Tagline |
|---|---|
| Primary | **"The Judgment Never Sleeps"** |
| Technical | **"Weighing Your Uptime"** |
| Short | **"The Uptime Judge"** |
| CLI banner | **"Every heartbeat, judged."** |
| Status page | **"Book of the Dead — All souls accounted for."** |
| Marketing | **"One binary. Eight protocols. Zero downtime."** |
| Developer | **"Your infrastructure's final judgment."** |

### 1.3 Brand Voice

**Tone:** Authoritative, ancient, inevitable — like death and judgment themselves. But with developer-friendly wit.

**Personality traits:**
- **Commanding** — AnubisWatch speaks with the authority of a god. "The judgment is final."
- **Vigilant** — Never sleeps, never misses. Every heartbeat is weighed.
- **Ancient wisdom** — Thousands of years of watching over the dead, now watching over your servers.
- **Dark humor** — "Your server has been devoured by Ammit." is a legitimate alert message.

**Do:**
- Use Egyptian mythology terminology consistently
- Mix ancient gravitas with modern dev culture
- Be direct and confident ("Your API is dead." not "Your API might be experiencing issues.")
- Use themed vocabulary: judgment, soul, verdict, resurrection, devouring

**Don't:**
- Be cutesy or overly playful
- Use generic tech jargon when a mythology term exists
- Break character — AnubisWatch is always the judge

### 1.4 Mythology Glossary (Brand Terminology)

| Standard Term | AnubisWatch Term | Origin |
|---|---|---|
| Monitor / Target | **Soul** | The entity being judged |
| Health Check | **Judgment** | The weighing of the heart |
| Probe Node | **Jackal** | Anubis's animal form |
| Cluster Leader | **Pharaoh** | Ruler of the Necropolis |
| Cluster | **Necropolis** | City of the Dead |
| Alert | **Verdict** | The judgment pronounced |
| Dashboard | **Hall of Ma'at** | The judgment hall |
| Status Page | **Book of the Dead** | Egyptian funerary text |
| Downtime | **Devouring** | Ammit devours the unworthy |
| Recovery | **Resurrection** | Return from the dead |
| Check Interval | **Weight** | How often the heart is weighed |
| Uptime % | **Purity** | Purity of the soul |
| Incident | **Curse** | Curse of the Pharaoh |
| Maintenance | **Embalming** | Preparing for the afterlife |
| Multi-step Check | **Duat Journey** | Journey through the underworld |
| Performance Budget | **Feather** | Feather of Ma'at (truth) |
| Node Join | **Summon** | Summoning a Jackal |
| Node Remove | **Banish** | Banishing from the Necropolis |
| Alive Status | **Aaru** | Egyptian paradise |
| Dead Status | **Ammit** | The soul-devourer |

---

## 2. COLOR PALETTE

### 2.1 Primary Colors

#### Anubis Gold
The gold of pharaohs' tombs and sacred artifacts.

| Shade | Hex | Usage |
|---|---|---|
| Gold 50 | `#FFF9E6` | Light backgrounds |
| Gold 100 | `#FFF0B3` | Hover states |
| Gold 200 | `#FFE066` | Badges, tags |
| Gold 300 | `#FFCC00` | Highlights |
| Gold 400 | `#E6B800` | Active states |
| **Gold 500** | **`#D4A843`** | **Primary brand color** |
| Gold 600 | `#B8922E` | Primary hover |
| Gold 700 | `#9C7B1A` | Primary active |
| Gold 800 | `#806500` | Text on light bg |
| Gold 900 | `#8B6914` | Dark accents |

#### Nile Blue
The sacred blue of the Nile, water of life.

| Shade | Hex | Usage |
|---|---|---|
| Blue 50 | `#E8F4FD` | Info backgrounds |
| Blue 100 | `#BAE0FA` | Info borders |
| Blue 200 | `#8CCAF5` | Links light |
| Blue 300 | `#5EB3F0` | Interactive elements |
| Blue 400 | `#3A9DE8` | Buttons secondary |
| **Blue 500** | **`#2563EB`** | **Secondary brand color** |
| Blue 600 | `#1D4ED8` | Links, CTAs |
| Blue 700 | `#1E40AF` | Active states |
| Blue 800 | `#1E3A8A` | Dark text |
| Blue 900 | `#1E3A5F` | Headings |

### 2.2 Accent Colors

#### Papyrus Sand
Ancient paper, warmth, earthiness.

| Shade | Hex | Usage |
|---|---|---|
| Sand 50 | `#FEFCF3` | Page backgrounds (light) |
| Sand 200 | `#EDE4CC` | Borders, dividers |
| **Sand 500** | **`#D4C5A0`** | **Accent color** |
| Sand 700 | `#A89871` | Secondary text |
| Sand 900 | `#8B7D5E` | Footer, muted |

### 2.3 Status Colors

| Status | Color | Hex | Meaning |
|---|---|---|---|
| Alive | Soul Green | `#22C55E` | Passed to Aaru (paradise) |
| Dead | Ammit Red | `#EF4444` | Devoured by Ammit |
| Degraded | Heavy Amber | `#F59E0B` | Heart is heavy |
| Embalmed | Royal Purple | `#8B5CF6` | Under maintenance |
| Unknown | Fog Gray | `#6B7280` | Not yet judged |

### 2.4 Theme Palettes

#### Dark Theme — Tomb Interior
The darkness inside a pharaoh's burial chamber.

| Token | Hex | Usage |
|---|---|---|
| `--tomb-bg` | `#0C0A09` | Page background |
| `--tomb-surface` | `#1C1917` | Card backgrounds |
| `--tomb-surface-hover` | `#292524` | Card hover |
| `--tomb-border` | `#44403C` | Borders |
| `--tomb-text` | `#FAFAF9` | Primary text |
| `--tomb-text-muted` | `#A8A29E` | Secondary text |
| `--tomb-gold-glow` | `rgba(212, 168, 67, 0.15)` | Gold glow effect |

#### Light Theme — Desert Sun
The blazing Egyptian desert under midday sun.

| Token | Hex | Usage |
|---|---|---|
| `--desert-bg` | `#FEFCE8` | Page background |
| `--desert-surface` | `#FFFFFF` | Card backgrounds |
| `--desert-surface-hover` | `#FEF9C3` | Card hover |
| `--desert-border` | `#E5E7EB` | Borders |
| `--desert-text` | `#1C1917` | Primary text |
| `--desert-text-muted` | `#6B7280` | Secondary text |

---

## 3. TYPOGRAPHY

### 3.1 Font Stack

| Purpose | Font | Fallback |
|---|---|---|
| UI / Body | **Inter** | system-ui, -apple-system, sans-serif |
| Code / Terminal | **JetBrains Mono** | ui-monospace, monospace |
| Display / Headers | **Inter** (weight 700-900) | system-ui |

### 3.2 Type Scale

| Element | Size | Weight | Line Height |
|---|---|---|---|
| Display (hero) | 48px / 3rem | 900 (Black) | 1.1 |
| H1 | 36px / 2.25rem | 800 (ExtraBold) | 1.2 |
| H2 | 30px / 1.875rem | 700 (Bold) | 1.25 |
| H3 | 24px / 1.5rem | 700 (Bold) | 1.3 |
| H4 | 20px / 1.25rem | 600 (SemiBold) | 1.35 |
| Body | 16px / 1rem | 400 (Regular) | 1.5 |
| Body Small | 14px / 0.875rem | 400 (Regular) | 1.5 |
| Caption | 12px / 0.75rem | 500 (Medium) | 1.4 |
| Code | 14px / 0.875rem | 400 (Regular) | 1.6 |
| CLI Output | 14px / 0.875rem | 400 (Regular) | 1.4 |

---

## 4. LOGO

### 4.1 Logo Concept

The AnubisWatch logo combines:
1. **Anubis jackal head** — Stylized geometric profile, facing left (traditional Egyptian direction)
2. **EKG heartbeat line** — Running horizontally through/below the jackal head, representing uptime monitoring
3. **Weighing scale** — Subtle integration into the jackal's profile or as a secondary element

### 4.2 Logo Variants

| Variant | Usage |
|---|---|
| **Full Logo** | Jackal head + "AnubisWatch" wordmark | Website, README, marketing |
| **Logo + Tagline** | Full logo + "The Judgment Never Sleeps" below | Landing page, presentations |
| **Icon Only** | Jackal head only | Favicon, app icon, social avatar |
| **Wordmark Only** | "AnubisWatch" text only | CLI banner, footer |
| **Monochrome** | Single color (gold or white) | Dark backgrounds, printing |

### 4.3 Logo Specifications

- **Minimum size:** 24px height (icon), 120px width (full logo)
- **Clear space:** Minimum 1x height of jackal head on all sides
- **Background:** Works on dark (tomb) and light (desert) backgrounds
- **File formats:** SVG (primary), PNG (2x, 4x), ICO (favicon)

### 4.4 Logo Colors

| Element | Dark BG | Light BG |
|---|---|---|
| Jackal head | Anubis Gold `#D4A843` | Anubis Gold `#D4A843` |
| EKG line | Soul Green `#22C55E` | Soul Green `#22C55E` |
| Wordmark | White `#FAFAF9` | Dark `#1C1917` |
| Tagline | Sand `#A8A29E` | Sand `#8B7D5E` |

### 4.5 Favicon

- Jackal head silhouette in Anubis Gold
- On dark background circle or transparent
- Sizes: 16x16, 32x32, 180x180 (Apple Touch), 192x192, 512x512 (PWA)

---

## 5. ICONOGRAPHY

### 5.1 Status Icons

| Status | Icon | Emoji | Description |
|---|---|---|---|
| Alive | `circle-check` | ✅ | Green filled circle with check |
| Dead | `skull` | 💀 | Red skull |
| Degraded | `alert-triangle` | ⚠️ | Amber triangle |
| Embalmed | `wrench` | 🔧 | Purple wrench |
| Unknown | `help-circle` | ❓ | Gray question mark |

### 5.2 Protocol Icons (Lucide React)

| Protocol | Icon |
|---|---|
| HTTP/HTTPS | `globe` |
| TCP | `plug` |
| UDP | `radio` |
| DNS | `at-sign` |
| SMTP | `mail` |
| IMAP | `inbox` |
| ICMP | `activity` |
| gRPC | `server` |
| WebSocket | `cable` |
| TLS | `shield-check` |

### 5.3 Navigation Icons (Lucide React)

| Page | Icon |
|---|---|
| Hall of Ma'at (Dashboard) | `layout-dashboard` |
| Souls (Monitors) | `heart-pulse` |
| Journeys (Synthetic) | `route` |
| Verdicts (Alerts) | `bell-ring` |
| Book of the Dead (Status) | `scroll-text` |
| Necropolis (Cluster) | `network` |
| Settings | `settings` |

---

## 6. UI PATTERNS

### 6.1 Card Style

```css
/* Soul card in dark theme */
.soul-card {
  background: var(--tomb-surface);
  border: 1px solid var(--tomb-border);
  border-radius: 12px;
  padding: 20px;
  transition: all 0.2s ease;
}

.soul-card:hover {
  border-color: var(--anubis-gold-500);
  box-shadow: 0 0 20px var(--tomb-gold-glow);
}

/* Status indicator glow */
.status-alive {
  box-shadow: 0 0 8px rgba(34, 197, 94, 0.4);
}

.status-dead {
  box-shadow: 0 0 8px rgba(239, 68, 68, 0.4);
  animation: pulse-red 2s infinite;
}
```

### 6.2 Egyptian Decorative Elements

- **Hieroglyphic dividers** — Subtle decorative line separators using Egyptian geometric patterns
- **Pyramid gradient** — Background gradient from dark base to lighter top (inverted pyramid)
- **Papyrus texture** — Very subtle paper texture overlay on light theme cards
- **Gold foil effect** — CSS gradient shimmer on primary CTAs
- **Eye of Horus** — Used as loading spinner or awareness indicator

### 6.3 Animation Guidelines

| Element | Animation | Duration |
|---|---|---|
| Soul status change | Color fade + glow pulse | 500ms |
| Heartbeat (alive) | EKG line animation (CSS) | 1.5s loop |
| Death (status → dead) | Red pulse + skull fade-in | 800ms |
| Resurrection | Green glow + check mark scale | 600ms |
| Loading | Eye of Horus rotation | 1s loop |
| Card hover | Border gold glow | 200ms |
| Page transition | Fade | 200ms |
| Alert pop-in | Slide-in from right | 300ms |

### 6.4 EKG Heartbeat Animation

```css
@keyframes heartbeat {
  0% { d: path("M 0 50 L 20 50 L 25 50 L 30 50 L 35 50 L 40 50 L 45 50"); }
  20% { d: path("M 0 50 L 20 50 L 25 20 L 30 80 L 35 10 L 40 50 L 45 50"); }
  40% { d: path("M 0 50 L 20 50 L 25 50 L 30 50 L 35 50 L 40 50 L 45 50"); }
  100% { d: path("M 0 50 L 20 50 L 25 50 L 30 50 L 35 50 L 40 50 L 45 50"); }
}
```

---

## 7. CLI BRANDING

### 7.1 CLI Banner

```
  ⚖️  AnubisWatch v1.0.0
  ─────────────────────────
  The Judgment Never Sleeps
```

### 7.2 CLI Color Scheme

| Element | Color (ANSI) |
|---|---|
| Banner / Title | Bold Gold (Yellow) |
| Success / Alive | Green |
| Error / Dead | Red |
| Warning / Degraded | Yellow |
| Info | Cyan |
| Muted / Labels | Gray |
| Table borders | Dark Gray |
| Embalmed | Magenta |

### 7.3 CLI Status Symbols

```
✅  Alive    (green circle check)
💀  Dead     (red skull)
⚠️  Degraded (yellow warning)
🔧  Embalmed (purple wrench)
❓  Unknown  (gray question)
⚖️  Judging  (scale — in progress)
👑  Pharaoh  (leader node)
🐺  Jackal   (follower node)
```

---

## 8. MARKETING ASSETS

### 8.1 Social Media

**Twitter/X:**
- Handle: `@AnubisWatch` (if available)
- Header: Anubis silhouette + EKG heartbeat line across full width
- Avatar: Jackal head icon (gold on dark)
- Bio: "⚖️ The Judgment Never Sleeps. Open-source uptime & synthetic monitoring. One binary, eight protocols, zero downtime. #AnubisWatch"

**GitHub:**
- Organization: `AnubisWatch`
- Social preview: Full logo + tagline + architecture diagram
- Topics: `monitoring`, `uptime`, `synthetic-monitoring`, `go`, `self-hosted`, `single-binary`

### 8.2 README Badge

```markdown
[![AnubisWatch](https://img.shields.io/badge/⚖️_AnubisWatch-The_Judgment_Never_Sleeps-D4A843?style=for-the-badge)](https://anubis.watch)
```

### 8.3 Status Page Badge

```html
<!-- Embeddable status badge -->
<a href="https://status.example.com">
  <img src="https://anubis.watch/badge/YOUR_PAGE_ID" alt="Status" />
</a>
```

Badge states:
- 🟢 "All Systems Operational" (green)
- 🟡 "Degraded Performance" (amber)
- 🔴 "Major Outage" (red)
- 🔧 "Under Maintenance" (purple)

### 8.4 Infographic Data Points

For Nano Banana 2 product infographic:

- **"8 Protocols, 1 Binary"** — HTTP, TCP, UDP, DNS, SMTP, ICMP, gRPC, WebSocket
- **"Zero Dependencies"** — Pure Go, single binary, no Node.js/Python/Java
- **"Raft Consensus"** — Every node is a probe AND a controller
- **"< 64MB RAM"** — Monitor 100+ targets on a Raspberry Pi
- **"CobaltDB Inside"** — Own embedded database, encrypted at rest
- **"React 19 Embedded"** — Beautiful dashboard compiled into the binary
- **"MCP-Native"** — AI agent integration out of the box
- **"Multi-Tenant"** — SaaS-ready workspace isolation

### 8.5 Competitor Comparison Visual

```
                    AnubisWatch    Uptime Kuma    UptimeRobot    Checkly
─────────────────── ──────────── ─────────────── ───────────── ──────────
Self-hosted         ✅ Yes        ✅ Yes          ❌ SaaS        ❌ SaaS
Single binary       ✅ Go         ❌ Node.js      ❌ N/A         ❌ N/A
Protocols           ✅ 8          ⚠️ 4-5          ❌ 1-2         ⚠️ 2
Distributed probes  ✅ Raft       ❌ No           ❌ No          ⚠️ SaaS
Synthetic checks    ✅ Yes        ❌ No           ❌ No          ✅ Yes
Embedded storage    ✅ CobaltDB   ⚠️ SQLite      ❌ N/A         ❌ N/A
Dashboard           ✅ React 19   ⚠️ Vue          ❌ Web         ✅ Web
Multi-tenant        ✅ Yes        ❌ No           ❌ No          ❌ No
MCP integration     ✅ Yes        ❌ No           ❌ No          ❌ No
Cost                ✅ Free       ✅ Free         ⚠️ Freemium    ❌ Paid
```

---

## 9. WEBSITE (anubis.watch)

### 9.1 Landing Page Sections

1. **Hero** — Jackal silhouette + "The Judgment Never Sleeps" + one-liner + CTA buttons (Get Started, GitHub)
2. **Problem** — "Your monitoring stack is a graveyard" — problems with existing tools
3. **Solution** — "One binary to judge them all" — key features with icons
4. **Architecture** — Animated Raft cluster diagram
5. **Protocols** — 8 protocol cards with hover details
6. **Dashboard Preview** — Screenshot/demo of Hall of Ma'at
7. **Comparison** — Feature table vs competitors
8. **Quick Start** — `curl | sh` + 3-step setup
9. **Community** — GitHub stars, contributors, Discord/Slack
10. **Footer** — Links, ECOSTACK TECHNOLOGY OÜ, license

### 9.2 Website Palette

Same as dashboard: Tomb Interior (dark) as primary theme with gold accents. The website should feel like entering an Egyptian tomb — dark, mysterious, with golden highlights illuminating the important elements.

---

## 10. OPEN SOURCE COMMUNITY

### 10.1 Contributing

- **CONTRIBUTING.md** — How to contribute (issues, PRs, code style)
- **CODE_OF_CONDUCT.md** — Contributor Covenant
- **Issue templates** — Bug report, Feature request, Question
- **PR template** — Description, type, checklist

### 10.2 Community Channels

- GitHub Discussions (primary)
- Discord server (if community grows)
- X/Twitter @AnubisWatch

### 10.3 Sticker / Swag Ideas

- Jackal head sticker (gold on black)
- "The Judgment Never Sleeps" text sticker
- "My servers are judged by Anubis" T-shirt
- Weighing scale with server heart emoji sticker
- "Soul: Alive ✅" / "Soul: Dead 💀" dual sticker

---

*The brand is eternal. The judgment is forever.* ⚖️
