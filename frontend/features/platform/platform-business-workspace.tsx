import { BusinessSettings } from "@/features/business/business-settings";
import type { BusinessSurface } from "@/lib/api/business";

export function PlatformBusinessWorkspace({ tenantID }: { tenantID: string }) {
  const surface: BusinessSurface = { kind: "platform", salonID: tenantID };

  return (
    <div className="space-y-5">
      <div>
        <h2 className="text-lg font-bold text-ink">Profile &amp; hours</h2>
        <p className="mt-1 text-sm text-muted">Manage salon identity, local business hours, and public-page settings. Staff, Services, and Customers each have their own workspace.</p>
      </div>
      <BusinessSettings surface={surface} title="Profile, hours & public page" />
    </div>
  );
}
