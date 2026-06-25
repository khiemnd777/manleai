"use client";

import { useEffect, useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { LockKeyhole, Mail, UserRound } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { apiRequest, setSession } from "@/lib/api/client";

type BootstrapStatus = {
  available: boolean;
};

type BootstrapOwnerResponse = {
  access_token: string;
  refresh_token: string;
};

type AccountForm = {
  email: string;
  full_name: string;
  password: string;
  confirm_password: string;
};

const initialForm: AccountForm = {
  email: "",
  full_name: "",
  password: "",
  confirm_password: ""
};

export function CreateAccountForm() {
  const router = useRouter();
  const [form, setForm] = useState<AccountForm>(initialForm);
  const [available, setAvailable] = useState<boolean | null>(null);
  const [loadingStatus, setLoadingStatus] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");

  useEffect(() => {
    let mounted = true;

    async function loadStatus() {
      setError("");
      setLoadingStatus(true);
      try {
        const status = await apiRequest<BootstrapStatus>("/api/auth/bootstrap/status");
        if (mounted) setAvailable(status.available);
      } catch (err) {
        if (mounted) setError(err instanceof Error ? err.message : "Could not load account setup status.");
      } finally {
        if (mounted) setLoadingStatus(false);
      }
    }

    void loadStatus();
    return () => {
      mounted = false;
    };
  }, []);

  const disabledReason = useMemo(() => {
    if (!form.email.trim() || !form.full_name.trim() || !form.password || !form.confirm_password) {
      return "Fill out all fields.";
    }
    if (form.password.length < 8) {
      return "Password must be at least 8 characters.";
    }
    if (form.password !== form.confirm_password) {
      return "Passwords do not match.";
    }
    return "";
  }, [form]);

  function setField(field: keyof AccountForm, value: string) {
    setForm((current) => ({ ...current, [field]: value }));
  }

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError("");
    setSuccess("");
    if (disabledReason) {
      setError(disabledReason);
      return;
    }

    setSubmitting(true);
    try {
      const response = await apiRequest<BootstrapOwnerResponse>("/api/auth/bootstrap-owner", {
        method: "POST",
        body: JSON.stringify({
          email: form.email,
          full_name: form.full_name,
          password: form.password
        })
      });
      setSession(response.access_token, response.refresh_token);
      setSuccess("Owner account created. Continue with salon setup.");
      router.push("/onboarding");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not create owner account.");
    } finally {
      setSubmitting(false);
    }
  }

  if (loadingStatus) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-11" />
        <Skeleton className="h-11" />
        <Skeleton className="h-11" />
        <Skeleton className="h-10" />
      </div>
    );
  }

  if (available === false) {
    return (
      <div className="space-y-4">
        <Alert
          title="Owner account setup is complete"
          message="Use the existing owner account to sign in. New owner accounts must be managed by an authenticated admin workflow."
        />
        <Link className="inline-flex text-sm font-semibold text-brand hover:text-teal-800" href="/login">
          Sign in
        </Link>
      </div>
    );
  }

  if (available === null) {
    return (
      <div className="space-y-4">
        <Alert
          title="Account setup unavailable"
          message={error || "Could not confirm whether first owner setup is available."}
        />
        <Button type="button" variant="secondary" onClick={() => window.location.reload()}>
          Retry
        </Button>
      </div>
    );
  }

  return (
    <form onSubmit={submit} className="space-y-4">
      {error ? <Alert title="Could not create account" message={error} /> : null}
      {success ? <Alert type="success" title="Account created" message={success} /> : null}
      <Field
        label="Email"
        type="email"
        autoComplete="email"
        icon={<Mail className="h-4 w-4 text-muted" />}
        value={form.email}
        onChange={(value) => setField("email", value)}
      />
      <Field
        label="Owner name"
        type="text"
        autoComplete="name"
        icon={<UserRound className="h-4 w-4 text-muted" />}
        value={form.full_name}
        onChange={(value) => setField("full_name", value)}
      />
      <Field
        label="Password"
        type="password"
        autoComplete="new-password"
        icon={<LockKeyhole className="h-4 w-4 text-muted" />}
        value={form.password}
        onChange={(value) => setField("password", value)}
      />
      <Field
        label="Confirm password"
        type="password"
        autoComplete="new-password"
        icon={<LockKeyhole className="h-4 w-4 text-muted" />}
        value={form.confirm_password}
        onChange={(value) => setField("confirm_password", value)}
      />
      <Button type="submit" className="w-full" disabled={submitting || Boolean(disabledReason)}>
        {submitting ? "Creating account..." : "Create owner account"}
      </Button>
      {disabledReason ? <p className="text-xs leading-5 text-muted">{disabledReason}</p> : null}
    </form>
  );
}

function Field({
  label,
  type,
  autoComplete,
  icon,
  value,
  onChange
}: {
  label: string;
  type: string;
  autoComplete: string;
  icon: React.ReactNode;
  value: string;
  onChange: (value: string) => void;
}) {
  return (
    <label className="block">
      <span className="text-sm font-semibold text-ink">{label}</span>
      <div className="mt-2 flex h-11 items-center gap-2 rounded-md border border-line bg-white px-3">
        {icon}
        <input
          className="w-full bg-transparent text-sm outline-none"
          value={value}
          onChange={(event) => onChange(event.target.value)}
          type={type}
          autoComplete={autoComplete}
          required
        />
      </div>
    </label>
  );
}
