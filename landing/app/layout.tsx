import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "Salon Services",
  description: "View salon services and staff, then call to request an appointment."
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
