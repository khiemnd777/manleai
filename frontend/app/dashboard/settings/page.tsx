import { DeferredPage } from "@/components/layout/deferred-page";

export default function SettingsPage() {
  return (
    <DeferredPage
      title="Settings"
      milestone="Foundation APIs available"
      description="Salon settings and business hours endpoints exist now. A full settings UI will be built around those endpoints in a later dashboard slice."
    />
  );
}

