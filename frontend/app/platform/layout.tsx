import { PlatformAppShell } from "@/components/layout/platform-app-shell";
import { SurfaceGate } from "@/components/layout/surface-gate";

export default function PlatformLayout({ children }: { children: React.ReactNode }) {
  return <SurfaceGate surface="platform"><PlatformAppShell>{children}</PlatformAppShell></SurfaceGate>;
}
