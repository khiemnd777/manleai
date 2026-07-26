import { AppShell } from "@/components/layout/app-shell";
import { SurfaceGate } from "@/components/layout/surface-gate";
import { TenantSalonProvider } from "@/components/layout/tenant-salon-context";

export default function DashboardLayout({ children }: { children: React.ReactNode }) {
  return <SurfaceGate surface="tenant"><TenantSalonProvider><AppShell>{children}</AppShell></TenantSalonProvider></SurfaceGate>;
}
