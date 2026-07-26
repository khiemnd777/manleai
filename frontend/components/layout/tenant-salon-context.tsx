"use client";

import { createContext, useContext, useEffect, useMemo, useState } from "react";
import { listBusinessSalons, type BusinessSalonSummary } from "@/lib/api/business";
import { persistActiveTenantSalonID, storedActiveTenantSalonID } from "@/lib/api/tenant-context";

type TenantSalonState = {
  salons: BusinessSalonSummary[];
  activeSalon: BusinessSalonSummary | null;
  activeSalonID: string;
  loading: boolean;
  error: string;
  setActiveSalonID: (salonID: string) => void;
  reload: () => void;
};

const TenantSalonContext = createContext<TenantSalonState | null>(null);

export function TenantSalonProvider({ children }: { children: React.ReactNode }) {
  const [salons, setSalons] = useState<BusinessSalonSummary[]>([]);
  const [activeSalonID, setActiveSalonIDState] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let current = true;
    setLoading(true);
    setError("");
    listBusinessSalons("tenant")
      .then((response) => {
        if (!current) return;
        setSalons(response.salons);
        setActiveSalonIDState((selected) => {
          const stored = storedActiveTenantSalonID();
          const next = response.salons.some((salon) => salon.id === selected)
            ? selected
            : response.salons.some((salon) => salon.id === stored) ? stored : response.salons[0]?.id ?? "";
          persistActiveTenantSalonID(next);
          return next;
        });
      })
      .catch((failure: unknown) => {
        if (current) setError(failure instanceof Error ? failure.message : "Could not load your salons.");
      })
      .finally(() => {
        if (current) setLoading(false);
      });
    return () => {
      current = false;
    };
  }, [attempt]);

  function setActiveSalonID(salonID: string) {
    if (salons.some((salon) => salon.id === salonID)) {
      persistActiveTenantSalonID(salonID);
      setActiveSalonIDState(salonID);
      window.location.reload();
    }
  }

  const value = useMemo<TenantSalonState>(() => ({
    salons,
    activeSalon: salons.find((salon) => salon.id === activeSalonID) ?? null,
    activeSalonID,
    loading,
    error,
    setActiveSalonID,
    reload: () => setAttempt((value) => value + 1)
  }), [activeSalonID, error, loading, salons]);

  return <TenantSalonContext.Provider value={value}>{children}</TenantSalonContext.Provider>;
}

export function useTenantSalon() {
  const value = useContext(TenantSalonContext);
  if (!value) throw new Error("useTenantSalon must be used inside TenantSalonProvider");
  return value;
}
