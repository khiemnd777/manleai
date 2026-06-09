import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "AI Receptionist for Nail Salons",
  description: "POS-first AI phone receptionist for nail salons using Square Appointments."
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}

