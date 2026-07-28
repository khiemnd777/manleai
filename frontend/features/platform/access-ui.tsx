"use client";

import { useEffect, useState } from "react";
import type { ReactNode } from "react";
import { Search } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { listPlatformAccessUsers, listTenantAccessUsers, type AccessUser } from "@/lib/api/access";

export function AccessUserSearch({
  value,
  onChange,
  scope,
  tenantID,
  excludeUserIDs = [],
  label = "Find an account",
  help = "Search by name or email. Only existing application accounts can receive access."
}: {
  value: string;
  onChange: (userID: string, user?: AccessUser) => void;
  scope: "tenant" | "platform";
  tenantID?: string;
  excludeUserIDs?: string[];
  label?: string;
  help?: string;
}) {
  const [query, setQuery] = useState("");
  const [users, setUsers] = useState<AccessUser[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  async function search(nextQuery = query) {
    setLoading(true);
    setError("");
    try {
      const result = scope === "platform"
        ? await listPlatformAccessUsers(nextQuery)
        : await listTenantAccessUsers(tenantID ?? "", nextQuery);
      if (result.users.some((user) => user.principal_scope !== scope)) {
        throw new Error("Account directory returned an identity from the wrong authorization realm.");
      }
      setUsers(result.users);
    } catch (failure) {
      setError(errorMessage(failure, "Could not search accounts."));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void search("");
  }, [scope, tenantID]);

  const excluded = new Set(excludeUserIDs);
  const options = users.filter((user) => !excluded.has(user.id) || user.id === value);

  return (
    <div className="space-y-2">
      <label className="block text-sm font-semibold text-ink">{label}</label>
      <div className="flex flex-col gap-2 sm:flex-row">
        <div className="flex h-10 min-w-0 flex-1 items-center gap-2 rounded-md border border-line bg-white px-3 focus-within:border-brand">
          <Search className="h-4 w-4 flex-none text-muted" />
          <input
            className="min-w-0 flex-1 bg-transparent text-sm text-ink outline-none"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                event.preventDefault();
                void search();
              }
            }}
            placeholder="Name or email"
            aria-label="Name or email"
          />
        </div>
        <Button type="button" variant="secondary" disabled={loading} onClick={() => void search()}>
          {loading ? "Searching…" : "Search"}
        </Button>
      </div>
      <select
        className="field"
        value={value}
        disabled={loading && users.length === 0}
        onChange={(event) => {
          const userID = event.target.value;
          onChange(userID, users.find((user) => user.id === userID));
        }}
      >
        <option value="">Select account</option>
        {options.map((user) => (
          <option key={user.id} value={user.id}>
            {accessUserLabel(user)} · {user.status}{user.data_classification === "sample_test" ? " · SAMPLE TEST" : ""}
          </option>
        ))}
      </select>
      <p className="text-xs leading-5 text-muted">{error || help}</p>
    </div>
  );
}

export function AccessUserSelect({
  users,
  value,
  onChange,
  label,
  emptyLabel = "No eligible accounts"
}: {
  users: AccessUser[];
  value: string;
  onChange: (userID: string) => void;
  label: string;
  emptyLabel?: string;
}) {
  return (
    <label className="block space-y-2">
      <span className="block text-sm font-semibold text-ink">{label}</span>
      <select className="field" value={value} onChange={(event) => onChange(event.target.value)}>
        <option value="">{users.length ? "Select account" : emptyLabel}</option>
        {users.map((user) => (
          <option key={user.id} value={user.id}>
            {accessUserLabel(user)}{user.data_classification === "sample_test" ? " · SAMPLE TEST" : ""}
          </option>
        ))}
      </select>
    </label>
  );
}

export function AccessRows({
  items,
  empty
}: {
  items: Array<{
    id: string;
    user: AccessUser;
    badges: string[];
    detail: ReactNode;
    action?: ReactNode;
  }>;
  empty: string;
}) {
  return (
    <div className="mt-4 divide-y divide-line overflow-hidden rounded-md border border-line">
      {items.length ? (
        items.map((item) => (
          <div key={item.id} className="flex flex-col gap-3 p-4 sm:flex-row sm:items-center sm:justify-between">
            <div className="min-w-0">
              <div className="flex flex-wrap items-center gap-2">
                <span className="break-words text-sm font-semibold text-ink">{accessUserLabel(item.user)}</span>
                {item.badges.map((badge) => <Badge key={badge} value={badge} />)}
                {item.user.data_classification === "sample_test" ? <Badge value="sample_test" /> : null}
              </div>
              <div className="mt-1 break-words text-xs leading-5 text-muted">{item.detail}</div>
            </div>
            {item.action ? <div className="flex-none sm:self-center [&>button]:w-full sm:[&>button]:w-auto">{item.action}</div> : null}
          </div>
        ))
      ) : (
        <p className="p-4 text-sm text-muted">{empty}</p>
      )}
    </div>
  );
}

export function accessUserLabel(user: AccessUser) {
  return `${user.full_name} · ${user.email}`;
}

export function errorMessage(error: unknown, fallback: string) {
  return error instanceof Error && error.message ? error.message : fallback;
}
