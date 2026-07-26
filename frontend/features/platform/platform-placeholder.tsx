import { Card, CardDescription, CardTitle } from "@/components/ui/card";

export function PlatformPlaceholder({title,description}:{title:string;description:string}){return <Card className="py-10"><CardTitle>{title}</CardTitle><CardDescription>{description}</CardDescription><p className="mt-4 text-sm leading-6 text-slate-600">This tab is intentionally separated from Tenant UI. It will only consume Platform-scoped APIs with exact salon capability checks.</p></Card>}
