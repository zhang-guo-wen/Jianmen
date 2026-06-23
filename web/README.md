# Jianmen Web Admin

Vue 3 + Vite + Element Plus admin frontend skeleton for the Jianmen service.

## Requirements

- Node.js 18+
- npm 9+

## Setup

```bash
npm install
npm run dev
```

For production build:

```bash
npm run build
```

## Configuration

Create `.env.local` from `.env.example` if the API server is not on the default URL.

```bash
VITE_API_BASE_URL=http://localhost:47100
```

The API client reads the auth token from `localStorage` key `jianmen_token` and sends it as a bearer token.

## Routes

- `/login`
- `/dashboard`
- `/hosts`
- `/sessions`
- `/rbac`
- `/audit`
- `/web-terminal`
