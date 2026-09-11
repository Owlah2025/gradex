"use client";

import * as React from "react";
import { Loader2, Monitor, Trash2 } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { listDevices, removeDevice, type DeviceOverview } from "@/lib/api/devices";
import { ProblemError } from "@/lib/api/problem";
import { currentCSRFToken } from "@/lib/identity/session";
import { useLocale } from "@/lib/i18n/locale-provider";

/**
 * The Student's own device list.
 *
 * It renders only what a person needs to recognise their two browsers and
 * decide which to give up: a coarse label, and when each was last used. It
 * never renders a device credential, a session identifier, a challenge, or the
 * address a device was last seen from — none of which would help the Student
 * and all of which would be a liability on a shared screen.
 */
export function DevicesPanel() {
  const { locale, t } = useLocale();
  const labels = t.devices;
  const [overview, setOverview] = React.useState<DeviceOverview | null>(null);
  const [error, setError] = React.useState<string | null>(null);
  const [removing, setRemoving] = React.useState<string | null>(null);
  const [confirming, setConfirming] = React.useState<string | null>(null);

  const load = React.useCallback(async () => {
    setError(null);
    try {
      setOverview(await listDevices(locale, currentCSRFToken() ?? ""));
    } catch {
      setError(labels.loadFailed);
    }
  }, [locale, labels.loadFailed]);

  React.useEffect(() => {
    void load();
  }, [load]);

  async function remove(deviceID: string, isCurrent: boolean) {
    setRemoving(deviceID);
    setError(null);
    try {
      await removeDevice(deviceID, locale, currentCSRFToken() ?? "");
      if (isCurrent) {
        // Removing the browser making the request ends its sessions
        // server-side. A full navigation is the honest response: this page's
        // in-memory session no longer authorizes anything.
        window.location.assign(`/${locale}/login?reason=signed-out`);
        return;
      }
      await load();
    } catch (problem) {
      setError(
        problem instanceof ProblemError &&
          problem.problem.code === "DEVICE_REPLACEMENT_COOLDOWN"
          ? labels.cooldownTitle
          : labels.removeFailed,
      );
    } finally {
      setRemoving(null);
      setConfirming(null);
    }
  }

  if (!overview && !error) {
    return (
      <p role="status" aria-live="polite" className="text-sm text-muted-foreground">
        <Loader2 aria-hidden className="me-2 inline size-4 animate-spin" />
      </p>
    );
  }

  const cooldownUntil = overview?.replacement_cooldown_until;

  return (
    <section data-testid="devices-panel" className="space-y-4">
      <div>
        <h2 className="text-lg font-semibold text-foreground">{labels.title}</h2>
        <p className="mt-1 text-sm text-muted-foreground">{labels.intro}</p>
      </div>

      {error ? <Alert tone="error" title={error} /> : null}

      {cooldownUntil ? (
        <Alert tone="info" title={labels.cooldownTitle}>
          {labels.cooldownBody.replace(
            "{until}",
            new Date(cooldownUntil).toLocaleString(locale === "ar" ? "ar" : "en"),
          )}
        </Alert>
      ) : null}

      {overview ? (
        <p data-testid="device-count" className="text-sm text-muted-foreground">
          {labels.limitLabel
            .replace("{used}", String(overview.devices.length))
            .replace("{limit}", String(overview.device_limit))}
        </p>
      ) : null}

      <ul className="space-y-3">
        {overview?.devices.length === 0 ? (
          <li className="text-sm text-muted-foreground">{labels.empty}</li>
        ) : null}
        {overview?.devices.map((device) => (
          <li
            key={device.id}
            data-testid="device-row"
            data-current={device.current_device ? "true" : "false"}
            className="flex flex-col gap-3 rounded-lg border border-border p-4 sm:flex-row sm:items-center sm:justify-between"
          >
            <div className="flex items-start gap-3">
              <Monitor aria-hidden className="mt-0.5 size-5 text-muted-foreground" />
              <div>
                <p className="text-sm font-medium text-foreground">
                  {device.label}
                  {device.current_device ? (
                    <span
                      data-testid="device-current-badge"
                      className="ms-2 rounded-full bg-muted px-2 py-0.5 text-xs font-normal text-muted-foreground"
                    >
                      {labels.currentDevice}
                    </span>
                  ) : null}
                </p>
                <p className="mt-1 text-xs text-muted-foreground">
                  {labels.lastActive}:{" "}
                  {new Date(device.last_active_at).toLocaleString(
                    locale === "ar" ? "ar" : "en",
                  )}
                </p>
              </div>
            </div>

            {confirming === device.id ? (
              <div className="flex flex-col items-start gap-2 sm:items-end">
                {/* Removing the current device signs this browser out. Saying so
                    before the Student confirms is the whole point: the action is
                    allowed, and it must not be a surprise. */}
                <p className="text-xs text-muted-foreground">
                  {device.current_device
                    ? labels.removeCurrentWarning
                    : labels.removeConfirm}
                </p>
                <div className="flex gap-2">
                  <Button type="button" variant="outline" onClick={() => setConfirming(null)}>
                    {labels.cancel}
                  </Button>
                  <Button
                    type="button"
                    data-testid="device-remove-confirm"
                    disabled={removing === device.id}
                    onClick={() => void remove(device.id, device.current_device)}
                  >
                    {removing === device.id ? labels.removing : labels.remove}
                  </Button>
                </div>
              </div>
            ) : (
              <Button
                type="button"
                variant="outline"
                data-testid="device-remove"
                data-device-id={device.id}
                onClick={() => setConfirming(device.id)}
              >
                <Trash2 aria-hidden className="me-2 size-4" />
                {labels.remove}
              </Button>
            )}
          </li>
        ))}
      </ul>
    </section>
  );
}
