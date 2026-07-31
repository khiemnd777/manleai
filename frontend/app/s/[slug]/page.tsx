import { redirect } from "next/navigation";
import { landingBaseUrl } from "@/lib/config/env";

export default async function PublicSalonRedirect({params}:{params:Promise<{slug:string}>}){
  const {slug}=await params;
  redirect(`${landingBaseUrl}/s/${encodeURIComponent(slug)}`);
}
