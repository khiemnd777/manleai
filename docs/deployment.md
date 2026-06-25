# Deployment

This release deploys the Go API, PostgreSQL, Redis, the owner admin dashboard,
the public salon landing app, and a Caddy reverse proxy.

## Production Domains

- Admin dashboard: `https://ai.knasoftware.com`
- API through the admin origin: `https://ai.knasoftware.com/api/*`
- Public salon landing app: `https://salon.knasoftware.com`
- Square redirect URL: `https://ai.knasoftware.com/api/integrations/square/callback`
- Voice public base URL: `https://ai.knasoftware.com`

Both DNS `A` records must point to the VPS before Caddy can issue certificates.

## GitHub Actions

The workflow in `.github/workflows/ci-cd.yml` runs on pull requests and pushes
to `main`. Pull requests and pushes run:

```bash
cd backend && go test ./...
cd frontend && npm ci && npm run typecheck && npm run build
cd landing && npm ci && npm run typecheck && npm run build
```

Pushes to `main`, and manual `workflow_dispatch` runs on `main`, also deploy to
the VPS by uploading a release archive, decoding `PROJECT_ENV_B64` into
`/opt/manleai/project.env`, and running:

```bash
docker compose --env-file /opt/manleai/project.env -f docker-compose.prod.yml -p manleai up -d --build --remove-orphans
```

Required GitHub Secrets:

```txt
SERVER_IP
REMOTE_USER
SSH_PASSWORD
PROJECT_ENV_B64
```

Use the VPS host and user values directly for `SERVER_IP` and `REMOTE_USER`.
Do not commit the SSH password or production environment file.

## Project Env

Create the production env file from the template:

```bash
cp deploy/project.env.example project.env
```

Generate replacement values for the required placeholders:

```bash
openssl rand -hex 24
openssl rand -base64 48
openssl rand -base64 32
```

Use the hex value for `POSTGRES_PASSWORD`, the 48-byte base64 value for
`JWT_SECRET`, and the 32-byte base64 value for `TOKEN_ENCRYPTION_KEY_BASE64`.
If the PostgreSQL password contains non URL-safe characters, URL-encode it in
`DATABASE_URL`; using the hex value avoids that issue.

After editing `project.env`, store it as a single-line base64 GitHub Secret:

```bash
gh secret set PROJECT_ENV_B64 --body "$(base64 -i project.env | tr -d '\n')"
```

Or paste the output of this command into the GitHub Secrets UI:

```bash
base64 -i project.env | tr -d '\n'
```

`project.env` is ignored by git.

## VPS Requirements

- Docker Engine with Docker Compose v2.
- Ports `80` and `443` open to the internet.
- DNS for `ai.knasoftware.com` and `salon.knasoftware.com` pointing to the VPS.
- No host-level nginx or Caddy service bound to `80` or `443`.

The deploy workflow checks for active nginx and host-level Caddy before
starting the Docker Caddy container. If nginx is serving existing Certbot sites,
the workflow fails before deployment so those sites can be migrated or nginx can
be stopped intentionally. Existing Certbot files under `/etc/letsencrypt` are
not modified by this deployment. Caddy manages HTTPS certificates for the two
ManleAI domains in its Docker volume.

## Dashboard-Managed Provider Configuration

Square Appointments, Twilio, and OpenAI credentials are configured in the
Integrations dashboard and stored encrypted per salon. The environment values
below are optional bootstrap or fallback values:

```txt
SQUARE_ENVIRONMENT
SQUARE_CLIENT_ID
SQUARE_CLIENT_SECRET
SQUARE_REDIRECT_URL
SQUARE_API_VERSION
VOICE_PROVIDER
VOICE_PUBLIC_BASE_URL
VOICE_TWILIO_AUTH_TOKEN
VOICE_TWILIO_INCOMING_PATH
VOICE_TWILIO_TURN_PATH
VOICE_TWILIO_RECORDING_PATH
VOICE_AI_PROVIDER
VOICE_OPENAI_API_KEY
VOICE_OPENAI_BASE_URL
VOICE_OPENAI_TRANSCRIPTION_MODEL
VOICE_OPENAI_REPLY_MODEL
VOICE_OPENAI_SPEECH_MODEL
VOICE_OPENAI_SPEECH_VOICE
```

## Production Rules

- Do not run `backend/seed/local.sql` in production.
- Do not log Square access or refresh tokens.
- Keep `AUTO_MIGRATE=true` unless another release process applies the same SQL migrations.
- Configure the Square redirect URL in the Integrations dashboard to the deployed API callback.
- Configure `VOICE_PUBLIC_BASE_URL` or the dashboard Twilio public base URL to the deployed API origin used in Twilio webhook settings.
- Keep Twilio and OpenAI secrets out of logs and docs; dashboard responses expose only configured/source metadata.
- Enable OpenAI voice AI in the Integrations dashboard only when external AI voice turns should be enabled.
- Keep OpenAI model and voice settings configurable through the dashboard so model changes do not require code changes.
- Restrict CORS to the deployed admin and landing origins.
