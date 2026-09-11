"use client";

import * as React from "react";
import { useRouter } from "next/navigation";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  listDevices,
  resendDeviceCode,
  trustDevice,
  type DeviceOverview,
  type SessionDeviceTrust,
} from "@/lib/api/devices";
import { ProblemError } from "@/lib/api/problem";
import { currentCSRFToken, deviceTrust } from "@/lib/identity/session";
import { useLocale } from "@/lib/i18n/locale-provider";

/**
 * Confirms a browser that has signed in but is not yet one of the Student's
 * trusted devices.
 *
 * The ordering is deliberate and matches what the server enforces. The password
 * has already been proven; the emailed code proves the mailbox; and only then
 * does the device limit enter the conversation. That is why a Student at the
 * limit is *not* met with a sign-in failure — they are told plainly that they
 * already have two devices, shown their own list, and asked to choose. Anyone
 * who reached this screen already holds a session, so showing them their own
 * devices discloses nothing they could not otherwise see.
 *
 * The device being confirmed is never named by this form. The server reads it
 * from the challenge and requires this browser to prove it holds that device's
 * credential, so a code cannot be aimed at a different browser.
 */
export function DeviceTrustForm() {
  const { locale, t } = useLocale();
  const labels = t.devices;
  const router = useRouter();
  const [trust] = React.useState<SessionDeviceTrust | null>(() => deviceTrust());
  const [code, setCode] = React.useState("");
  const [replaceDeviceID, setReplaceDeviceID] = React.useState("");
  const [overview, setOverview] = React.useState<DeviceOverview | null>(null);
  const [error, setError] = React.useState<string | null>(null);
  const [notice, setNotice] = React.useState<string | null>(null);
  const [submitting, setSubmitting] = React.useState(false);
  const inFlight = React.useRef(false);

  const atLimit = trust?.admission === "LIMIT_REACHED";
  const challengeID = trust?.challenge?.challenge_id ?? "";
  const maskedEmail = trust?.challenge?.masked_email ?? "";

  React.useEffect(() => {
    if (!atLimit) return;
    void listDevices(locale, currentCSRFToken() ?? "")
      .then(setOverview)
      .catch(() => setError(labels.loadFailed));
  }, [atLimit, locale, labels.loadFailed]);

  // A browser that arrives here with no outstanding challenge has nothing to
  // confirm. Sending it back to sign in is the only honest move; inventing a
  // challenge identifier would produce a form that can never succeed.
  if (!trust || !challengeID) {
    return (
      <Alert tone="info" title={labels.trustUnavailable}>
        {""}
      </Alert>
    );
  }

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    if (inFlight.current) return;
    setError(null);
    setNotice(null);
    if (atLimit && !replaceDeviceID) {
      setError(labels.limitNeedsChoice);
      return;
    }
    inFlight.current = true;
    setSubmitting(true);
    try {
      await trustDevice(
        challengeID,
        code,
        locale,
        currentCSRFToken() ?? "",
        atLimit ? replaceDeviceID : undefined,
      );
      // The session this browser already holds has been upgraded server-side.
      // A full navigation re-resolves it rather than trusting this page's
      // in-memory copy, which still describes the pending state.
      window.location.assign(`/${locale}/learn/dashboard`);
    } catch (problem) {
      setCode("");
      setError(messageFor(problem, labels));
    } finally {
      inFlight.current = false;
      setSubmitting(false);
    }
  }

  async function resend() {
    setError(null);
    setNotice(null);
    try {
      await resendDeviceCode(challengeID, locale, currentCSRFToken() ?? "");
      setNotice(labels.trustResent);
    } catch (problem) {
      setError(messageFor(problem, labels));
    }
  }

  return (
    <form className="space-y-5" onSubmit={submit} noValidate data-testid="device-trust-form">
      {atLimit ? (
        <Alert tone="info" title={labels.limitTitle}>
          {labels.limitBody.replace("{email}", maskedEmail)}
        </Alert>
      ) : (
        <p className="text-sm text-muted-foreground">
          {labels.trustIntro.replace("{email}", maskedEmail)}
        </p>
      )}

      {error ? <Alert tone="error" title={error} /> : null}
      {notice ? <Alert tone="success" title={notice} /> : null}

      {atLimit ? (
        <fieldset className="space-y-2">
          <legend className="text-sm font-medium text-foreground">{labels.limitChoose}</legend>
          {overview?.devices.map((device) => (
            <label
              key={device.id}
              data-testid="replaceable-device"
              className="flex items-center gap-3 rounded-lg border border-border p-3 text-sm"
            >
              <input
                type="radio"
                name="replace_device_id"
                value={device.id}
                checked={replaceDeviceID === device.id}
                onChange={() => setReplaceDeviceID(device.id)}
              />
              <span className="text-foreground">{device.label}</span>
              <span className="ms-auto text-xs text-muted-foreground">
                {labels.lastActive}:{" "}
                {new Date(device.last_active_at).toLocaleString(
                  locale === "ar" ? "ar" : "en",
                )}
              </span>
            </label>
          ))}
        </fieldset>
      ) : null}

      <Field label={labels.trustCode} htmlFor="device-code">
        <Input
          id="device-code"
          data-testid="device-code"
          name="code"
          inputMode="numeric"
          autoComplete="one-time-code"
          maxLength={12}
          value={code}
          onChange={(event) => setCode(event.target.value)}
        />
      </Field>

      <div className="flex flex-wrap items-center gap-3">
        <Button type="submit" data-testid="device-trust-submit" disabled={submitting}>
          {submitting ? labels.trustSubmitting : labels.trustSubmit}
        </Button>
        <Button type="button" variant="outline" onClick={() => void resend()}>
          {labels.trustResend}
        </Button>
      </div>
    </form>
  );
}

function messageFor(problem: unknown, labels: { trustInvalid: string; trustExhausted: string; trustUnavailable: string; cooldownTitle: string; limitNeedsChoice: string }): string {
  if (!(problem instanceof ProblemError)) return labels.trustUnavailable;
  switch (problem.problem.code) {
    case "VALIDATION_FAILED":
      return labels.trustInvalid;
    case "RATE_LIMITED":
      return labels.trustExhausted;
    case "DEVICE_REPLACEMENT_COOLDOWN":
      return labels.cooldownTitle;
    case "DEVICE_LIMIT_REACHED":
      return labels.limitNeedsChoice;
    default:
      return labels.trustUnavailable;
  }
}
