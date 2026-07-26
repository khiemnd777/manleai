"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { getCurrentSession, isPlatformSession, type CurrentSession } from "@/lib/api/session";

export function SurfaceGate({ surface, children }: { surface: "tenant" | "platform"; children: React.ReactNode }) {
  const router = useRouter();
  const [session, setSession] = useState<CurrentSession | null>(null);
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let current = true;
    setError("");
    getCurrentSession()
      .then((value) => {
        if (!current) return;
        if (surface === "platform" && !isPlatformSession(value)) {
          router.replace(value.salon_id ? "/dashboard" : "/login");
          return;
        }
        if (surface === "tenant" && !value.salon_id) {
          router.replace(isPlatformSession(value) ? "/platform/tenants" : "/onboarding");
          return;
        }
        setSession(value);
      })
      .catch((failure: unknown) => {
        if (current) setError(failure instanceof Error ? failure.message : "Could not verify this workspace.");
      });
    return () => {
      current = false;
    };
  }, [attempt, router, surface]);

  if (error) {
    return (
      <main className="mx-auto flex min-h-screen max-w-xl items-center px-5 py-12">
        <Alert title="Workspace unavailable" message={error}>
          <Button type="button" variant="secondary" className="mt-4" onClick={() => setAttempt((value) => value + 1)}>
            Retry
          </Button>
        </Alert>
      </main>
    );
  }
  if (!session) {
    return (
      <main className="mx-auto max-w-4xl space-y-4 px-5 py-12" aria-label="Loading workspace">
        <Skeleton className="h-10 w-64" />
        <Skeleton className="h-28 w-full" />
        <Skeleton className="h-64 w-full" />
      </main>
    );
  }
  return children;
}
