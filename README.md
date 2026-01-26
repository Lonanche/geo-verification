# GeoGuessr Verification Service

A Cloudflare Workers service that verifies GeoGuessr users by checking if they can send a verification code via GeoGuessr direct messages.

## How It Works

1. Your backend calls `/api/v1/verify/start` with a GeoGuessr user ID
2. User receives a 6-character verification code
3. User sends a friend request to the bot account on GeoGuessr
4. User sends the verification code via GeoGuessr DM
5. Service automatically accepts friend requests and monitors chat messages
6. When code is found, session is marked as verified
7. Optional webhook callback is sent to your backend

## Setup

### Prerequisites

- Node.js 18+
- Cloudflare account
- GeoGuessr account (for the bot)

### Installation

```bash
npm install
```

### Configuration

#### Local Development

Create `.dev.vars` file:

```env
# Required: GeoGuessr authentication token (from browser cookie _ncfa)
GEOGUESSR_NCFA_TOKEN=your_token_here

# Optional: API key for authentication (if not set, API is open)
API_KEY=your-secret-api-key

# Optional overrides:
# CODE_EXPIRY_MINUTES=5
# RATE_LIMIT_PER_HOUR=3
# ALLOWED_CALLBACK_HOSTS=localhost,127.0.0.1
```

To get the `GEOGUESSR_NCFA_TOKEN`:
1. Log in to geoguessr.com
2. Open DevTools (F12) → Application → Cookies → www.geoguessr.com
3. Copy the `_ncfa` cookie value

#### Production

Set secrets via Cloudflare dashboard or CLI:

```bash
wrangler secret put GEOGUESSR_NCFA_TOKEN
wrangler secret put API_KEY
```

Update `wrangler.toml` for production settings:

```toml
[vars]
CODE_EXPIRY_MINUTES = "5"
RATE_LIMIT_PER_HOUR = "3"
ALLOWED_CALLBACK_HOSTS = "your-domain.com"
```

### Running

```bash
# Development
npm run dev

# Deploy to Cloudflare
npm run deploy
```

## API Reference

### Authentication

All API endpoints (except `/health`) require the `X-API-Key` header if `API_KEY` is configured:

```
X-API-Key: your-secret-api-key
```

### Endpoints

#### Start Verification

```http
POST /api/v1/verify/start
Content-Type: application/json
X-API-Key: your-api-key

{
  "user_id": "geoguessr-user-id",
  "callback_url": "https://your-domain.com/webhook"  // optional
}
```

Response:
```json
{
  "session_id": "uuid",
  "verification_code": "ABC123",
  "expires_at": "2024-01-01T00:00:00.000Z"
}
```

#### Check Status

```http
GET /api/v1/verify/status/{session_id}
X-API-Key: your-api-key
```

Response:
```json
{
  "session_id": "uuid",
  "user_id": "geoguessr-user-id",
  "verified": true,
  "expires_at": "2024-01-01T00:00:00.000Z",
  "created_at": "2024-01-01T00:00:00.000Z"
}
```

#### Health Check

```http
GET /health
```

Response:
```json
{
  "status": "ok"
}
```

### Webhook Callback

If `callback_url` is provided, a POST request is sent when verification completes or expires:

```json
{
  "session_id": "uuid",
  "user_id": "geoguessr-user-id",
  "status": "verified",  // or "expired"
  "timestamp": "2024-01-01T00:00:00.000Z"
}
```

## Rate Limiting

- Default: 3 verification attempts per user per hour (per GeoGuessr user ID)
- Configurable via `RATE_LIMIT_PER_HOUR`

## Security

### API Key Protection

Always set `API_KEY` in production. Without it, anyone can use your verification service.

### Protecting Against Abuse

The built-in rate limiting is per GeoGuessr user ID. If your frontend allows users to trigger verification, implement additional rate limiting in your backend (per authenticated user, per IP, etc.) before calling this service.

### WAF Rules (Recommended for Production)

To block unauthorized requests at the edge (before they hit your Worker):

1. Add a custom domain to your worker (e.g., `verify.yourdomain.com`)
2. Go to Cloudflare Dashboard → your domain → Security → WAF → Custom Rules
3. Create a rule to block requests without valid API key:
   - Expression: `(http.host eq "verify.yourdomain.com" and not any(http.request.headers["x-api-key"][*] eq "your-secret-key"))`
   - Action: Block

This prevents cost from malicious requests - they're blocked at Cloudflare's edge for free.

## Architecture

- **Cloudflare Workers** - Serverless compute
- **Durable Objects** - State management
  - `VerificationManager` - Sessions, friend requests, chat monitoring
  - `UserRateLimiter` - Per-user rate limiting with token bucket algorithm
