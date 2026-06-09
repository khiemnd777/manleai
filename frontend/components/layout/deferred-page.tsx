import { Card, CardDescription, CardTitle } from "@/components/ui/card";

export function DeferredPage({
  title,
  milestone,
  description
}: {
  title: string;
  milestone: string;
  description: string;
}) {
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-ink">{title}</h1>
        <p className="mt-1 text-sm text-muted">{milestone}</p>
      </div>
      <Card>
        <CardTitle>Scope boundary</CardTitle>
        <CardDescription>{description}</CardDescription>
      </Card>
    </div>
  );
}

