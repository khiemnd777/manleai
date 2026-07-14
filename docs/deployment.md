# Deployment

This release deploys the Go API, background worker, PostgreSQL, Redis, the
owner admin dashboard, the public salon landing app, and the POS calendar app.
Public HTTP and HTTPS are owned by the shared VPS edge gateway, not by the
ManleAI Docker Compose project.

## Production Domains

- Admin dashboard: `https://ai.knasoftware.com`
- API through the admin origin: `https://ai.knasoftware.com/api/*`
- Public salon landing app: `https://salon.knasoftware.com`
- POS calendar app: `https://pos.knasoftware.com`
- Square redirect URL: `https://ai.knasoftware.com/api/integrations/square/callback`
- Voice public base URL: `https://ai.knasoftware.com`

All DNS `A` records must point to the VPS before Caddy can issue certificates.

## Edge Gateway

The VPS has one shared systemd Caddy service. It owns ports `80` and `443`; its
managed root config imports `/etc/caddy/projects/*.caddy`. ManleAI does not run
its own Caddy container and must never edit `/etc/caddy/Caddyfile` directly.

`project-edgectl` is the only supported project route manager. It stores each
project route under `/etc/caddy/projects/<project-id>.caddy`, records ownership
in `/var/lib/project-edge/registry/<project-id>.json`, validates the combined
Caddy config, rejects domain or upstream conflicts, backs up the project route,
and reloads Caddy with rollback on failure.

ManleAI exposes:

```txt
127.0.0.1:18089 -> api:8080
127.0.0.1:13088 -> frontend:3000
127.0.0.1:13090 -> landing:3000
127.0.0.1:13091 -> pos-calendar:3000
```

The CI/CD deploy job renders `deploy/manleai.caddy.template` and
`deploy/manleai.edge-manifest.template`, then runs:

```bash
sudo -n project-edgectl validate manleai <route-file> <manifest-file>
sudo -n project-edgectl upsert manleai <route-file> <manifest-file>
```

`upsert` runs only after the ManleAI API loopback healthcheck succeeds. It can
change only the ManleAI route and manifest; existing project route files remain
owned by their registered projects.

## GitHub Actions

The workflow in `.github/workflows/ci-cd.yml` runs only when a release tag
matching `v*` is pushed. Commit pushes to `main` do not trigger this workflow.
The normal release command is:

```bash
make release TAG=v2026.06.25.1
```

That command requires a clean `main` worktree, creates an annotated git tag, and
pushes the tag to `origin`. The tag push runs backend tests, typechecks and
builds the web apps, then builds and publishes the five application images to
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
for image in manleai-api manleai-worker manleai-frontend manleai-landing manleai-pos-calendar; do
  docker pull "ghcr.io/khiemnd777/$image:<release-tag>"
done
docker logout ghcr.io
docker compose \
  --env-file /opt/manleai/project.env \
  --env-file /opt/manleai/releases/<release>/images.env \
  -f docker-compose.prod.yml -p manleai up -d --no-build --remove-orphans
sudo -n project-edgectl validate manleai <route-file> <manifest-file>
sudo -n project-edgectl upsert manleai <route-file> <manifest-file>
```

The workflow uses an SSH private key for the deploy identity. The GHCR token is
written to a mode-`600` temporary file on the VPS only for `docker login`, then
removed on normal success and failure paths; diagnostics also attempt removal.
It is never added to `project.env`, committed to the repository, passed as a
remote command argument, or left in Docker's credential store after
`docker logout`. The workflow uses the repository-scoped `GITHUB_TOKEN`; no
additional long-lived GHCR credential is required.

The VPS pulls release images one at a time to bound memory, CPU, network, and
disk-extraction pressure. It does not prune shared Docker build cache. Docker
Compose starts containers only after every required image is present locally.
During the first GHCR migration, an old running container can refer to image
metadata that Docker no longer retains. The workflow reports that case and
proceeds without automatic image rollback; after the first successful GHCR
release, every running application image is tag-addressable and rollback is
available again.

If the remote deploy command fails or its SSH session disconnects, the workflow
opens a short diagnostic SSH session and reports memory, deploy-path disk
usage, Docker disk usage, Compose status, and bounded application logs. It does
not print `project.env` or any deployment secret. If that follow-up connection
also fails, the original deploy failure remains the authoritative result.

Required GitHub Secrets:

```txt
SERVER_IP
REMOTE_USER
SSH_PRIVATE_KEY
PROJECT_ENV_B64
```

Use the VPS host and dedicated deploy identity for `SERVER_IP` and
`REMOTE_USER`. `SSH_PRIVATE_KEY` must be the matching private key. Do not
commit the private key or production environment file.

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
- DNS for `ai.knasoftware.com`, `salon.knasoftware.com`, and
  `pos.knasoftware.com` pointing to the VPS before certificate issuance.
- Systemd Caddy active with managed root `/etc/caddy/Caddyfile` and
  `project-edgectl` installed.
- A dedicated deploy identity that can access Docker, owns `/opt/manleai`, and
  has passwordless sudo only for `/usr/local/bin/project-edgectl`.
- SSH public-key access for that deploy identity; the matching private key is
  stored only in the `SSH_PRIVATE_KEY` GitHub secret.

Existing project Caddy routes are not modified by this deployment. Do not
install another Caddy container, restart Caddy manually, or bind application
containers to ports `80` or `443`.

## Dashboard-Managed Provider Configuration

Square Appointments, Twilio, and OpenAI credentials are configured in the
Integrations dashboard and stored encrypted per salon in
`salon_integration_configs`. Repository env files and env templates contain
infrastructure settings only; do not add provider credentials, provider model
settings, webhook paths, transports, or provider URLs to them.

For active-runtime diagnosis, use `/dashboard/integrations`,
`GET /api/salons/:id/integration-configs`, the relevant readiness/debug endpoint
such as `GET /api/salons/:id/voice/status`, persisted provider records, and the
runtime resolver code. Never use `project.env`, `.env`, Compose defaults,
GitHub secrets, or process environment values as evidence of the active
salon-scoped provider configuration.

The backend retains legacy environment fallback code for compatibility. That
path is not the normal production configuration workflow and is intentionally
absent from repository env templates. Inspect it only for an explicitly scoped
bootstrap/legacy task after proving that the salon has no stored provider
configuration.

The configured OpenAI reply model may serve two guarded roles: style-only
rewriting for eligible safe replies, and strict structured interpretation of
every freeform turn that reaches conversation orchestration. Turn input is limited to a PII-reduced caller utterance,
boolean customer-field presence, selected/catalog service and staff references,
party-group references, pending act, booking action, dialog phase, and draft revision. The backend
rejects low-confidence goals, invalid entity/operation combinations, malformed
counts, and non-catalog IDs. The model cannot mutate the draft, call booking,
or produce confirmed appointment state. When the provider is disabled or
unavailable, ambiguous mutations preserve the current draft and enter safe
clarification/handoff behavior.

## Consultation Data Pack After Deployment

Schema migration V38 creates the consultation profile table, but schema
migrations do not contain tenant-specific salon data. To reproduce the investor
demo catalog configuration without manual re-entry:

1. Deploy the release and connect the target salon to Square Appointments.
2. Sync Square until the seven target services exist with matching names and
   durations and are active-provider, POS-linked, synced, and AI-bookable.
3. In `/dashboard/settings`, open Configuration transfer and choose
   `docs/lotus-investor-demo-consultation-pack-v7.json`.
4. Confirm the pack contains 5 service categories and 20 category aliases;
   Preview reports 25 taxonomy creates, 7 service alias creates, and 7 service
   consultation profile creates with zero conflicts, then Apply once.
5. Verify the consultation toggle and profile coverage in Settings before the
   production call test.

The scoped v7 pack does not overwrite salon profile, integration configuration,
AI runtime settings, secrets, services, or POS mappings. Missing, ambiguous, or
ineligible profile targets block Apply. Reapplying the same payload is
idempotent and does not create duplicate profiles or increment unchanged profile
revisions.

## Production Rules

- Do not run `backend/seed/local.sql` in production.
- Do not log Square access or refresh tokens.
- Keep `AUTO_MIGRATE=true` for the API unless another release process applies
  the same SQL migrations. The worker runs with `AUTO_MIGRATE=false` and starts
  only after the API healthcheck succeeds.
- Configure the Square redirect URL in the Integrations dashboard to the deployed API callback.
- Configure the dashboard Twilio public base URL to the deployed API origin used in Twilio webhook settings.
- Configure realtime phone mode from the Integrations dashboard: Twilio `voice_transport=realtime_stream`; OpenAI realtime model/voice and noise profile for input; and `speech_output_mode=streaming_tts` for low-latency backend-approved output. `buffered_realtime` is a legacy rollback mode.
- Keep Twilio and OpenAI secrets out of logs and docs; dashboard responses expose only configured/source metadata.
- Enable OpenAI voice AI in the Integrations dashboard only when external AI voice turns should be enabled.
- Keep OpenAI model and voice settings configurable through the dashboard so model changes do not require code changes.
- Restrict CORS to the deployed admin, landing, and POS calendar origins.
