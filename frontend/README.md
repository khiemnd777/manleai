# Frontend

Next.js admin frontend for the AI Receptionist commercial production foundation.

## Commands

```bash
npm install
npm run dev
npm run typecheck
npm run build
```

## Environment

```bash
NEXT_PUBLIC_API_BASE_URL=http://localhost:18089
```

## Implemented Screens

- `/create-account`
- `/login`
- `/onboarding`
- `/dashboard`
- `/dashboard/appointments`
- `/dashboard/billing`
- `/dashboard/calls`
- `/dashboard/customers`
- `/dashboard/integrations`
- `/dashboard/services`
- `/dashboard/settings`
- `/dashboard/staff`
- `/dashboard/training`

The sidebar includes operational pages plus gated future product areas. Gated
pages show the real dependency and do not fake data or production behavior.

The public customer-facing catalog is a separate Next.js app in `landing/`.
