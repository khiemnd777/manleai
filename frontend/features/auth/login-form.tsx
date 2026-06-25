"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { LockKeyhole, Mail } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Alert } from "@/components/ui/alert";
import { apiRequest, setSession } from "@/lib/api/client";

type LoginResponse = {
  access_token: string;
  refresh_token: string;
};

type BootstrapStatus = {
  available: boolean;
};

export function LoginForm() {
  const router = useRouter();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [bootstrapAvailable, setBootstrapAvailable] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    let mounted = true;

    async function loadBootstrapStatus() {
      try {
        const status = await apiRequest<BootstrapStatus>("/api/auth/bootstrap/status");
        if (mounted) setBootstrapAvailable(status.available);
      } catch {
        if (mounted) setBootstrapAvailable(false);
      }
    }

    void loadBootstrapStatus();
    return () => {
      mounted = false;
    };
  }, []);

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError("");
    setLoading(true);
    try {
      const response = await apiRequest<LoginResponse>("/api/auth/login", {
        method: "POST",
        body: JSON.stringify({ email, password })
      });
      setSession(response.access_token, response.refresh_token);
      router.push("/dashboard");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Login failed.");
    } finally {
      setLoading(false);
    }
  }

  return (
    <form onSubmit={submit} className="space-y-4">
      {error ? <Alert title="Could not sign in" message={error} /> : null}
      <label className="block">
        <span className="text-sm font-semibold text-ink">Email</span>
        <div className="mt-2 flex h-11 items-center gap-2 rounded-md border border-line bg-white px-3">
          <Mail className="h-4 w-4 text-muted" />
          <input
            className="w-full bg-transparent text-sm outline-none"
            value={email}
            onChange={(event) => setEmail(event.target.value)}
            type="email"
            autoComplete="email"
            required
          />
        </div>
      </label>
      <label className="block">
        <span className="text-sm font-semibold text-ink">Password</span>
        <div className="mt-2 flex h-11 items-center gap-2 rounded-md border border-line bg-white px-3">
          <LockKeyhole className="h-4 w-4 text-muted" />
          <input
            className="w-full bg-transparent text-sm outline-none"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            type="password"
            autoComplete="current-password"
            required
          />
        </div>
      </label>
      <Button type="submit" className="w-full" disabled={loading}>
        {loading ? "Signing in..." : "Sign in"}
      </Button>
      {bootstrapAvailable ? (
        <p className="text-sm leading-6 text-muted">
          First owner account is not set up yet.{" "}
          <Link className="font-semibold text-brand hover:text-teal-800" href="/create-account">
            Create account
          </Link>
        </p>
      ) : null}
    </form>
  );
}
