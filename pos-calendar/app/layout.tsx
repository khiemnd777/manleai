import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "POS Calendar",
  description: "POS-first calendar view for Square Appointments salons."
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
