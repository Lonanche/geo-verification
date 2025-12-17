# GeoGuessr Verification API

Verify GeoGuessr users via friend requests and chat messages with real-time webhook notifications.

## Setup

```bash
export GEOGUESSR_NCFA_TOKEN="your_ncfa_token_here"  # Get from browser cookies
go run main.go
```

**Get NCFA token:** Login to GeoGuessr → DevTools (F12) → Application → Cookies → Copy `_ncfa` value

## How It Works

1. **Start verification** - POST to `/api/v1/verify/start` with user_id + callback_url
2. **Get code** - API returns verification code (e.g. "a25660")
3. **User adds bot** - User adds bot as friend on GeoGuessr
4. **User sends code** - User DMs the code to bot on GeoGuessr
5. **Auto-verify** - Background service detects message and verifies
6. **Webhook sent** - Your app gets real-time notification

## API

**Start Verification:**
```bash
curl -X POST localhost:8080/api/v1/verify/start \
  -d '{"user_id":"USER_ID","callback_url":"https://yourapp.com/webhook"}'
# Returns: {"verification_code":"a25660","session_id":"uuid",...}
```

**Webhook Payload:**
```json
{"session_id":"uuid","user_id":"USER_ID","status":"verified","timestamp":"2024-12-16T12:30:45Z"}
{"session_id":"uuid","user_id":"USER_ID","status":"expired","timestamp":"2024-12-16T12:30:45Z"}
```

**User Instructions:**
```html
Add @bot as friend on GeoGuessr and send: a25660
```

**Features:**
- Auto-accepts friend requests
- Monitors chat messages (3s intervals)
- Single session per user
- 5min expiry + rate limiting