# Deployment

This release is deployment-ready in structure, but not yet pilot-ready for booking calls.

## Required Services

- Go API container
- PostgreSQL
- Redis
- Next.js frontend

## Required Secrets

```txt
DATABASE_URL
REDIS_URL
JWT_SECRET
TOKEN_ENCRYPTION_KEY_BASE64
AUTO_MIGRATE
SQUARE_CLIENT_ID
SQUARE_CLIENT_SECRET
SQUARE_REDIRECT_URL
VOICE_PROVIDER
VOICE_PUBLIC_BASE_URL
VOICE_TWILIO_AUTH_TOKEN
CORS_ALLOWED_ORIGINS
FRONTEND_URL
```

Use a 32-byte base64 value for `TOKEN_ENCRYPTION_KEY_BASE64`.

## Production Rules

- Do not run `backend/seed/local.sql` in production.
- Do not log Square access or refresh tokens.
- Keep `AUTO_MIGRATE=true` unless another release process applies the same SQL migrations.
- Configure the Square redirect URL to the deployed API callback.
- Configure `VOICE_PUBLIC_BASE_URL` to the deployed API origin used in Twilio webhook settings.
- Keep `VOICE_TWILIO_AUTH_TOKEN` secret; it is used only for Twilio webhook signature verification.
- Restrict CORS to the deployed frontend URL.
