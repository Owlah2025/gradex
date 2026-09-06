"use client";

import { useState } from "react";

/** Each immutable source can fail once; changing the source permits a fresh load. */
export function ThumbnailImage({ src, className = "", onError }: {
  src: string; className?: string; onError?: () => void;
}) {
  const [failedSource, setFailedSource] = useState<string | null>(null);
  if (failedSource === src) return null;
  // Native images use Gradex's authorized same-origin delivery route directly.
  // eslint-disable-next-line @next/next/no-img-element
  return <img src={src} alt="" loading="lazy" decoding="async" className={className}
    onError={() => { setFailedSource(src); onError?.(); }} />;
}
