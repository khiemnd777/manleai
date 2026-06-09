import { DeferredPage } from "@/components/layout/deferred-page";

export default function BillingPage() {
  return (
    <DeferredPage
      title="Billing"
      milestone="Scheduled after pilot readiness"
      description="The first foundation release does not include Stripe or subscription state. Billing will remain explicitly separated from POS booking behavior."
    />
  );
}

