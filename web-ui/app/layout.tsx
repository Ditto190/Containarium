import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "Containarium - Container Management",
  description: "Web UI for managing Incus/LXC containers",
};

// Runs synchronously before React hydrates — prevents flash of wrong theme.
const themeScript = `(function(){var t=localStorage.getItem('theme')||'dark';if(t==='light')document.documentElement.classList.add('light');})();`;

export default function RootLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="en" suppressHydrationWarning>
      <head>
        <script dangerouslySetInnerHTML={{ __html: themeScript }} />
      </head>
      <body>{children}</body>
    </html>
  );
}
