"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { LockKeyhole, Mail } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Alert } from "@/components/ui/alert";
import { apiRequest, setSession } from "@/lib/api/client";

type LoginResponse = {
  access_token: string;
  refresh_token: string;
};

export function LoginForm() {
  const router = useRouter();
  const [email, setEmail] = useState("owner@lotusnails.example");
  const [password, setPassword] = useState("password123");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

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
          />
        </div>
      </label>
      <Button type="submit" className="w-full" disabled={loading}>
        {loading ? "Signing in..." : "Sign in"}
      </Button>
    </form>
  );
}

