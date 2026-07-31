"use client";

import { CheckCircle2, KeyRound, Loader2 } from "lucide-react";
import Link from "next/link";
import { useEffect, useState } from "react";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { acceptOwnerInvitation } from "@/lib/api/tenant-registration";
import { ownerInvitationTokenFromFragment } from "@/lib/api/tenant-registration-routes";

export function AcceptOwnerInvitationForm() {
  const [token, setToken] = useState("");
  const [password, setPassword] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [loading, setLoading] = useState(false);
  const [complete, setComplete] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    const invitationToken = ownerInvitationTokenFromFragment(window.location.hash);
    setToken(invitationToken);
    window.history.replaceState(null, "", window.location.pathname);
  }, []);

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    if (!token) return setError("This invitation link is incomplete.");
    if (password.length < 12) return setError("Use at least 12 characters.");
    if (password !== confirmation) return setError("Passwords do not match.");
    setLoading(true); setError("");
    try {
      await acceptOwnerInvitation(token, password);
      setToken(""); setPassword(""); setConfirmation(""); setComplete(true);
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "Invitation acceptance failed.");
    } finally { setLoading(false); }
  }

  if (complete) return <div className="text-center"><CheckCircle2 className="mx-auto h-12 w-12 text-emerald-600"/><h1 className="mt-5 text-2xl font-bold text-ink">Owner account activated</h1><p className="mt-3 text-sm leading-6 text-muted">Your password is set and this invitation cannot be reused.</p><Link href="/login" className="mt-6 inline-flex h-10 items-center rounded-md bg-brand px-5 text-sm font-semibold text-white">Sign in</Link></div>;

  return <form onSubmit={submit} className="space-y-5"><div className="text-center"><KeyRound className="mx-auto h-10 w-10 text-brand"/><h1 className="mt-4 text-2xl font-bold text-ink">Activate your Owner account</h1><p className="mt-2 text-sm leading-6 text-muted">Create a password for the Tenant identity selected during salon setup.</p></div>{!token?<Alert title="Invitation token missing" message="Ask ManleAI Operations for a fresh invitation link."/>:null}{error?<Alert title="Could not activate account" message={error}/>:null}<label className="block"><span className="text-sm font-semibold text-ink">Password</span><input className="field mt-2" type="password" autoComplete="new-password" minLength={12} maxLength={128} required value={password} onChange={event=>setPassword(event.target.value)}/><span className="mt-1 block text-xs text-muted">At least 12 characters.</span></label><label className="block"><span className="text-sm font-semibold text-ink">Confirm password</span><input className="field mt-2" type="password" autoComplete="new-password" minLength={12} maxLength={128} required value={confirmation} onChange={event=>setConfirmation(event.target.value)}/></label><Button className="w-full" type="submit" disabled={!token||loading}>{loading?<Loader2 className="h-4 w-4 animate-spin"/>:null}{loading?"Activating…":"Activate account"}</Button><p className="text-center text-xs leading-5 text-muted">The link expires after 72 hours and can be used once. ManleAI never asks for provider credentials on this page.</p></form>;
}
