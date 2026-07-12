# Shelf

> Your server, your modules, your rules.

Shelf is a lightweight, modular home server framework. Define your data, write your logic, design your pages — Shelf handles everything else.

## What is Shelf?

Shelf is a framework for building your own personal server platform. Instead of using someone else's app, you create exactly what you need as **modules** — and Shelf takes care of deployment, routing, authentication, database management, and all the production concerns automatically.

```
You write:           Shelf handles:
─────────────        ──────────────────────────────────
schema.ts            DB creation, migrations, CRUD API
logic.ts             Routing, error handling, scheduling
pages/               SSR, layout shell, navigation
manifest.json        Auth, API keys, sidebar menu, permissions
```

## Philosophy

- **Module developers only care about 3 things**: screens, logic, and data.
- **Everything else is the framework's job**: auth, error handling, logging, rate limiting, response formatting, deployment.
- **Modules are isolated**: each module gets its own database, its own storage, its own routes. No conflicts, no dependencies.
- **One server, one process, one container**: runs on a Raspberry Pi, scales to whatever you need.

## Quick start

```bash
# Install
curl -fsSL https://get.shelf.dev | sh

# Or with Docker
docker run -p 9666:9666 -v ./data:/app/data shelf/core

# Create your first module
npx shelf create weather

# Start developing
npm run dev
# → http://localhost:9666/weather
```

## Creating a module

A module is just 4 files:

```
plugins/weather/
├── manifest.json      # "What am I" — name, icon, menu, external APIs
├── schema.ts          # "What data do I have" — table definitions
├── logic.ts           # "What do I do" — business functions
└── pages/
    └── index.tsx      # "How do I look" — React components
```

### manifest.json

```json
{
  "name": "weather",
  "displayName": "Weather",
  "icon": "cloud",
  "menu": [
    { "label": "Dashboard", "path": "/" },
    { "label": "Settings", "path": "/settings" }
  ],
  "externalApis": [
    {
      "key": "openweather",
      "baseUrl": "https://api.openweathermap.org/data/2.5",
      "auth": { "type": "query", "param": "appid" }
    }
  ]
}
```

### schema.ts

```typescript
import { defineTable, text, integer } from '@shelf/core'

export const cities = defineTable('cities', {
  id: integer().primaryKey(),
  name: text().notNull(),
  lat: text(),
  lon: text(),
})

export const records = defineTable('records', {
  id: integer().primaryKey(),
  cityId: integer().references(cities.id),
  temp: integer(),
  condition: text(),
  recordedAt: integer({ mode: 'timestamp' }),
})
```

Shelf automatically:
- Creates `weather.db`
- Runs migrations
- Generates CRUD API at `/api/weather/cities` and `/api/weather/records`

### logic.ts

```typescript
import { cities, records } from './schema'

export default ({ db, api, notify }) => ({
  async fetchWeather(cityId: number) {
    const city = await db.find(cities, cityId)
    const data = await api.openweather.get('/weather', {
      params: { lat: city.lat, lon: city.lon }
    })
    return db.create(records, {
      cityId,
      temp: data.main.temp,
      condition: data.weather[0].main,
    })
  },

  jobs: [
    { cron: '0 * * * *', handler: 'fetchWeather' }
  ]
})
```

### pages/index.tsx

```tsx
import { usePage, Card } from '@shelf/ui'

export default function WeatherDashboard() {
  const { data: cities } = usePage('getCities')

  return (
    <div>
      <h1>Weather</h1>
      <div className="grid">
        {cities.map(city => (
          <Card key={city.id}>
            <Card.Title>{city.name}</Card.Title>
            <Card.Value>{city.latestTemp}°C</Card.Value>
          </Card>
        ))}
      </div>
    </div>
  )
}
```

That's it. No routing setup, no error handling, no auth code, no database connection logic.

## What Shelf handles for you

| Concern | How |
|---|---|
| Database | Per-module SQLite file, auto-created and migrated |
| API | CRUD auto-generated from schema, custom endpoints from logic |
| Auth | Session-based, role-based access control, middleware injected |
| Errors | Throw → standard JSON error response, auto-logged |
| Validation | Zod schema → auto 400 response with details |
| External APIs | Declare in manifest → core manages keys, auth, retries |
| Notifications | `ctx.notify.send()` — push, websocket, configurable |
| File storage | Scoped per module, path traversal protected |
| Scheduling | Declare cron in logic → core runs it |
| Logging | Structured, scoped by module, auto request logging |
| Rate limiting | Configurable per module, sane defaults |
| UI shell | Sidebar + topbar auto-rendered, modules fill content area |

## Architecture

```
Browser
  │
  ▼
┌─────────────────────────────────────────┐
│ Core Framework                           │
│                                          │
│ Request Pipeline (auto)                  │
│ CORS → Rate Limit → Auth → Timeout      │
│                                          │
│ Router (auto-mount)                      │
│ /admin/* │ /api/{mod}/* │ /{mod}/*       │
│                                          │
│ Services (injected)                      │
│ Auth │ DB │ Events │ Notify │ Storage    │
│ External API │ Scheduler │ Config │ Log  │
│                                          │
│ Auto-generated Layer                     │
│ CRUD API │ Response envelope │ SSR shell │
├──────────────────────────────────────────┤
│ Module Code (you write this)             │
│ schema.ts │ logic.ts │ pages/            │
└──────────────────────────────────────────┘
  │
  ▼
[blog.db] [weather.db] [todo.db] [core.db]
```

## Module communication

Modules don't import each other. They communicate through an event bus:

```typescript
// weather module emits
ctx.events.emit('weather:updated', { city: 'Seoul', temp: 28 })

// dashboard module listens
ctx.events.on('weather:updated', (data) => {
  updateWidget(data)
})
```

## Tech stack

- **Runtime**: Node.js + TypeScript
- **HTTP**: Hono
- **ORM**: Drizzle
- **Database**: SQLite (per module)
- **Frontend**: React SSR (MPA)
- **Validation**: Zod
- **Deploy**: Single Docker container

## Requirements

- Node.js 20+
- npm 10+

## License

MIT
