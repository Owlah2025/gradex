"use client";

import { useState } from "react";
import { requestMaterialDownload } from "@/lib/api/learning";
import { describeApiError } from "@/lib/api/api-error";
import { currentCSRFToken } from "@/lib/identity/session";

type MaterialDownloadProps = {
  authorizationPath: string;
  title: string;
  locale: "ar" | "en";
  downloadLabel: string;
  preparingLabel: string;
  unavailableLabel: string;
};

/**
 * Gets a new private-download capability immediately before navigation.
 *
 * The server must still authorize the opaque attachment path; the component
 * holds no storage key, asset version, or persistent signed URL. A failure to
 * issue that capability stays on the Lesson page with localized recovery copy.
 */
export function MaterialDownload({
  authorizationPath,
  title,
  locale,
  downloadLabel,
  preparingLabel,
  unavailableLabel,
}: MaterialDownloadProps) {
  const [state, setState] = useState<"idle" | "preparing" | "failed">("idle");
  const [message, setMessage] = useState("");

  const download = async () => {
    setState("preparing");
    setMessage("");
    try {
      const issued = await requestMaterialDownload(authorizationPath, locale, currentCSRFToken());
      // This is the only point a short-lived storage URL reaches the browser.
      // Navigation leaves no durable copy in application state.
      window.location.assign(issued.url);
    } catch (error) {
      setState("failed");
      setMessage(describeApiError(error, locale) || unavailableLabel);
    }
  };

  return (
    <div className="flex flex-wrap items-center justify-between gap-3">
      <span className="min-w-0 truncate text-sm font-medium text-foreground">{title}</span>
      <button
        type="button"
        onClick={() => void download()}
        disabled={state === "preparing"}
        aria-label={`${downloadLabel}: ${title}`}
        className="shrink-0 rounded-md border border-border px-3 py-2 text-sm font-semibold text-foreground hover:bg-accent disabled:opacity-60 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
      >
        {state === "preparing" ? preparingLabel : downloadLabel}
      </button>
      {state === "failed" ? (
        <p role="alert" className="basis-full text-sm text-destructive">
          {message || unavailableLabel}
        </p>
      ) : null}
    </div>
  );
}
