# Local Development

## Prerequisites

- Go 1.23+
- Node 20+
- Docker
- PostgreSQL client tools for `make seed-local`

## Start Services

```bash
cp .env.example .env
docker compose up -d --build
```

## Seed Local Owner

```bash
cd backend
DATABASE_URL=postgres://ai_receptionist:ai_receptionist@localhost:55432/ai_receptionist?sslmode=disable make seed-local
```

## Run API

```bash
cp backend/.env.example backend/.env
cd backend
make run-api
```

## Run Frontend

```bash
cd frontend
npm install
npm run dev
```

Visit `http://localhost:3000/login`.

## Notes

Mock data is not the production path. The local seed creates a demo owner and salon only so the dashboard can be exercised without manual SQL.
