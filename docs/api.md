# API

Base URL: `http://localhost:18080`

All endpoints except login, refresh, logout, health, and the Square OAuth callback require:

```txt
Authorization: Bearer <access_token>
```

## Auth

`POST /api/auth/login`

```json
{
  "email": "owner@lotusnails.example",
  "password": "password123"
}
```

Returns access token, refresh token, user, roles, and primary salon ID.

`POST /api/auth/refresh-token`

```json
{
  "refresh_token": "..."
}
```

`POST /api/auth/logout`

```json
{
  "refresh_token": "..."
}
```

`GET /api/auth/me`

## Salons

`GET /api/salons`

Returns salons owned by the authenticated user.

`POST /api/salons`

```json
{
  "name": "Lotus Nails Studio",
  "phone": "+13125550101",
  "address": "1200 W Sample Ave",
  "city": "Chicago",
  "state": "IL",
  "zip_code": "60601",
  "timezone": "America/Chicago",
  "primary_language": "en",
  "secondary_language": "vi",
  "handoff_phone": "+13125550102"
}
```

`GET /api/salons/:id/settings`

`PUT /api/salons/:id/settings`

`GET /api/salons/:id/business-hours`

`PUT /api/salons/:id/business-hours`

## Square

`GET /api/integrations/square/connect-url?salon_id=<id>`

Returns a Square OAuth URL and state.

`GET /api/integrations/square/callback?code=<code>&state=<state>`

Exchanges the Square OAuth code and stores encrypted tokens.

`GET /api/integrations/square/status?salon_id=<id>`

Returns the Square connection and recent sync logs.

`GET /api/integrations/square/locations?salon_id=<id>`

Lists Square locations through the Square adapter.

`POST /api/integrations/square/select-location`

```json
{
  "salon_id": "...",
  "location_id": "..."
}
```

`POST /api/integrations/square/sync`

```json
{
  "salon_id": "..."
}
```
