import { PublicSalonPage } from "@/features/public/public-salon-page";

export default function SalonLandingPage({params}:{params:{slug:string}}){return <PublicSalonPage slug={params.slug}/>}
