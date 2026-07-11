# Deployment

This release deploys the Go API, PostgreSQL, Redis, the owner admin dashboard,
the public salon landing app, and the POS calendar app. Public HTTP and HTTPS
are owned by the shared VPS edge gateway, not by the ManleAI Docker Compose
project.

## Production Domains

- Admin dashboard: `https://ai.knasoftware.com`
- API through the admin origin: `https://ai.knasoftware.com/api/*`
- Public salon landing app: `https://salon.knasoftware.com`
- POS calendar app: `https://pos.knasoftware.com`
- Square redirect URL: `https://ai.knasoftware.com/api/integrations/square/callback`
- Voice public base URL: `https://ai.knasoftware.com`

All DNS `A` records must point to the VPS before Caddy can issue certificates.

## Edge Gateway

The VPS has one shared Caddy gateway under `/opt/edge-gateway`. It owns ports
`80` and `443` and routes public domains to localhost ports exposed by app
stacks. ManleAI does not run its own Caddy container.

ManleAI exposes:

```txt
127.0.0.1:18089 -> api:8080
127.0.0.1:13088 -> frontend:3000
127.0.0.1:13090 -> landing:3000
127.0.0.1:13091 -> pos-calendar:3000
```

The CI/CD deploy job renders `deploy/manleai.caddy.template` into
`/opt/edge-gateway/conf.d/manleai.caddy`, validates the full Caddy config inside
the `edge-gateway-caddy` container, and reloads Caddy only after validation
passes. If validation fails, the previous ManleAI route file is restored.

## GitHub Actions

The workflow in `.github/workflows/ci-cd.yml` runs only when a release tag
matching `v*` is pushed. Commit pushes to `main` do not trigger this workflow.
The normal release command is:

```bash
make release TAG=v2026.06.25.1
```

That command requires a clean `main` worktree, creates an annotated git tag, and
pushes the tag to `origin`. The tag push runs backend tests, typechecks and
builds the web apps, then builds and publishes the four application images to
GitHub Container Registry (GHCR) with that immutable release tag. The VPS
receives only the Compose/Caddy deployment bundle, `project.env`, and the image
tag metadata. It logs into GHCR with the job's ephemeral `GITHUB_TOKEN`, pulls
the tagged images, logs out, and starts the stack without compiling application
source code. Each image carries the `org.opencontainers.image.source` label for
this repository, so the GHCR package inherits repository access permissions.

Every deploy SSH/SCP connection has a 15-second connection timeout and sends
keepalives during long-running work. Before replacing containers, the VPS tags
the currently running application images locally. If the API healthcheck or
edge-gateway validation/reload fails, it restores those images and keeps the
previous `/opt/manleai/current` release active. Only after both checks succeed
does it move `/opt/manleai/current` to the new release:

```bash
docker login ghcr.io
docker builder prune --all --force
for image in manleai-api manleai-frontend manleai-landing manleai-pos-calendar; do
  docker pull "ghcr.io/khiemnd777/$image:<release-tag>"
done
docker logout ghcr.io
docker compose \
  --env-file /opt/manleai/project.env \
  --env-file /opt/manleai/releases/<release>/images.env \
  -f docker-compose.prod.yml -p manleai up -d --no-build --remove-orphans
docker exec edge-gateway-caddy caddy validate --config /etc/caddy/Caddyfile
docker exec edge-gateway-caddy caddy reload --config /etc/caddy/Caddyfile
```

The GHCR credential exists only in the encrypted SSH session. It is never added
to `project.env`, committed to the repository, or left in Docker's credential
store after `docker logout`. The workflow uses the repository-scoped
`GITHUB_TOKEN`; no additional long-lived GitHub secret is required.

The VPS pulls release images one at a time to bound memory, CPU, network, and
disk-extraction pressure on the 2 GB host. Docker Compose starts containers only
after every required image is present locally.

`docker builder prune --all --force` removes only unused BuildKit cache from the
former VPS-build workflow; it does not remove running containers, their images,
or Docker volumes. During the first GHCR migration, an old running container can
refer to image metadata that Docker no longer retains. The workflow reports that
case and proceeds without automatic image rollback; after the first successful
GHCR release, every running application image is tag-addressable and rollback
is available again.

If the remote deploy command fails or its SSH session disconnects, the workflow
opens a short diagnostic SSH session and reports memory, deploy-path disk
usage, Docker disk usage, Compose status, and bounded application logs. It does
not print `project.env` or any deployment secret. If that follow-up connection
also fails, the original deploy failure remains the authoritative result.

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
- Shared edge gateway running as container `edge-gateway-caddy`.
- Edge gateway compose file at `/opt/edge-gateway/docker-compose.yml`.

Existing Certbot files under `/etc/letsencrypt` are not modified by this
deployment. Caddy manages HTTPS certificates in its Docker data volume.
Do not restart app-owned Caddy containers such as `perfect-pitch-caddy-1`; the
shared edge gateway is now the only container that should bind `80` and `443`.

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
VOICE_TWILIO_STREAM_PATH
VOICE_TWILIO_VOICE_TRANSPORT
VOICE_AI_PROVIDER
VOICE_OPENAI_API_KEY
VOICE_OPENAI_BASE_URL
VOICE_OPENAI_TRANSCRIPTION_MODEL
VOICE_OPENAI_REPLY_MODEL
VOICE_OPENAI_SPEECH_MODEL
VOICE_OPENAI_SPEECH_VOICE
VOICE_OPENAI_REALTIME_ENABLED
VOICE_OPENAI_REALTIME_MODEL
VOICE_OPENAI_REALTIME_VOICE
VOICE_OPENAI_REALTIME_INSTRUCTIONS
```

Do not troubleshoot active Square, Twilio, or OpenAI provider behavior by
editing `project.env` before checking the Integrations dashboard. When a salon
has dashboard-saved provider configuration, that encrypted salon-scoped config
takes precedence over `project.env`, `.env`, and GitHub deployment secrets.
Keep provider secrets out of production env files unless intentionally using
the legacy fallback path for a fresh deployment with no saved dashboard config.

## Production Rules

- Do not run `backend/seed/local.sql` in production.
- Do not log Square access or refresh tokens.
- Keep `AUTO_MIGRATE=true` unless another release process applies the same SQL migrations.
- Configure the Square redirect URL in the Integrations dashboard to the deployed API callback.
- Configure the dashboard Twilio public base URL to the deployed API origin used in Twilio webhook settings; `VOICE_PUBLIC_BASE_URL` is only the fallback source when no dashboard Twilio config exists.
- Configure realtime phone mode from the Integrations dashboard: Twilio `voice_transport=realtime_stream`, Twilio stream path, OpenAI realtime model/voice, and the realtime noise profile. The env names above are fallback-only values.
- Keep Twilio and OpenAI secrets out of logs and docs; dashboard responses expose only configured/source metadata.
- Enable OpenAI voice AI in the Integrations dashboard only when external AI voice turns should be enabled.
- Keep OpenAI model and voice settings configurable through the dashboard so model changes do not require code changes.
- Restrict CORS to the deployed admin, landing, and POS calendar origins.
