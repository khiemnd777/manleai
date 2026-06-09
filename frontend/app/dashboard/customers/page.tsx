import { DeferredPage } from "@/components/layout/deferred-page";

export default function CustomersPage() {
  return (
    <DeferredPage
      title="Customers"
      milestone="Scheduled for Milestone 3"
      description="Customer search and creation will be handled through the POSProvider boundary so Square customer payloads remain inside the Square adapter."
    />
  );
}

