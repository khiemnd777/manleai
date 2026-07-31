import { notFound, redirect } from "next/navigation";
import { getDefaultPublicCatalog, PublicApiError } from "@/lib/api";
export const dynamic="force-dynamic";
export default async function SalonHostHome(){let catalog;try{catalog=await getDefaultPublicCatalog()}catch(error){if(error instanceof PublicApiError&&error.status===404)notFound();throw error}redirect(`/s/${catalog.salon.slug}`)}
