import { ImageResponse } from "next/og";
import { siteConfig } from "@/config/site";

export const runtime = "edge";
export const alt = "Gradex — Graduate with excellence";
export const size = { width: 1200, height: 630 };
export const contentType = "image/png";

// Dynamic social-share image (no static binary asset to maintain).
export default function OgImage() {
  return new ImageResponse(
    (
      <div
        style={{
          width: "100%",
          height: "100%",
          display: "flex",
          flexDirection: "column",
          justifyContent: "center",
          padding: "80px",
          background:
            "radial-gradient(120% 90% at 85% 10%, #1e4ed8 0%, #0d1b2a 55%)",
          color: "#ffffff",
          fontFamily: "sans-serif",
        }}
      >
        <div style={{ display: "flex", alignItems: "center", gap: 20 }}>
          <svg width="72" height="72" viewBox="0 0 100 100">
            <path d="M50 22 L72 58 L50 51 L28 58 Z" fill="#4f7cff" />
            <path d="M50 51 L72 58 L57 76 Z" fill="#1e4ed8" />
            <path d="M50 51 L28 58 L43 76 Z" fill="#a8c1ff" />
            <path d="M72 58 L84 55 L77 64 Z" fill="#ff7e4d" />
          </svg>
          <div style={{ fontSize: 44, fontWeight: 800 }}>
            Grade<span style={{ color: "#ff7e4d" }}>x</span>
          </div>
        </div>
        <div
          style={{
            marginTop: 40,
            fontSize: 76,
            fontWeight: 800,
            lineHeight: 1.05,
            maxWidth: 900,
          }}
        >
          Graduate with excellence.
        </div>
        <div
          style={{
            marginTop: 28,
            fontSize: 30,
            color: "rgba(255,255,255,0.82)",
            maxWidth: 860,
          }}
        >
          {siteConfig.description}
        </div>
      </div>
    ),
    { ...size },
  );
}
