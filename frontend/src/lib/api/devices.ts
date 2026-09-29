import { authenticatedRequest } from "./http";

/**
 * The Student trusted-device surface.
 *
 * Nothing here is a secret. The device credential itself lives in an HttpOnly
 * `__Host-` cookie that JavaScript can never read, and every call below is
 * authorized by the ordinary session cookie — the device credential only tells
 * the server *which* of the Student's browsers is asking.
 */

export type DeviceTrustState =
  | "NOT_APPLICABLE"
  | "PENDING_DEVICE_TRUST"
  | "TRUSTED"
  | "LEGACY_UNBOUND";

export type DeviceAdmission = "TRUSTED";

export type SessionDeviceTrust = {
  state: DeviceTrustState;
  admission?: DeviceAdmission;
};

/**
 * One trusted device as the Student sees it.
 *
 * Deliberately absent: the device credential, its digest, any session
 * identifier, and the IP address it was last seen from. A device list is a
 * recognition aid, not a security dossier, and the Student only needs enough to
 * tell their two browsers apart.
 */
export type TrustedDevice = {
  id: string;
  label: string;
  browser_family: string;
  platform_family: string;
  trusted_at: string;
  last_active_at: string;
  current_device: boolean;
};

export type DeviceOverview = {
  devices: TrustedDevice[];
  device_limit: number;
  replacement_cooldown_until?: string;
  replacement_ready: boolean;
};

export type DeviceRemoved = {
  removed: boolean;
  /** True when the Student removed the browser they are using, which ends it. */
  signed_out: boolean;
};

export function listDevices(locale: "ar" | "en", csrf: string) {
  return authenticatedRequest<DeviceOverview>(
    "/me/devices",
    "GET",
    locale,
    csrf,
  ) as Promise<DeviceOverview>;
}

export function removeDevice(
  deviceID: string,
  locale: "ar" | "en",
  csrf: string,
) {
  return authenticatedRequest<DeviceRemoved>(
    `/me/devices/${encodeURIComponent(deviceID)}`,
    "DELETE",
    locale,
    csrf,
  ) as Promise<DeviceRemoved>;
}

export function listAdminDevices(accountID: string, locale: "ar" | "en") {
  return authenticatedRequest<{
    devices: (TrustedDevice & { state: string; first_seen_at?: string; revoked_at?: string | null; revocation_reason?: string })[];
    device_limit: number;
    replacement_cooldown_until?: string | null;
  }>(`/admin/students/${encodeURIComponent(accountID)}/devices`, "GET", locale) as Promise<{
    devices: (TrustedDevice & { state: string; first_seen_at?: string; revoked_at?: string | null; revocation_reason?: string })[];
    device_limit: number;
    replacement_cooldown_until?: string | null;
  }>;
}

export function revokeAdminDevice(accountID: string, deviceID: string, locale: "ar" | "en", csrf: string) {
  return authenticatedRequest<{ revoked: number }>(
    `/admin/students/${encodeURIComponent(accountID)}/devices/${encodeURIComponent(deviceID)}/revocations`,
    "POST", locale, csrf,
  );
}

export function revokeAllAdminDevices(accountID: string, locale: "ar" | "en", csrf: string) {
  return authenticatedRequest<{ revoked: number }>(
    `/admin/students/${encodeURIComponent(accountID)}/devices/revocations`, "POST", locale, csrf,
  );
}

export function resetAdminDeviceCooldown(accountID: string, locale: "ar" | "en", csrf: string) {
  return authenticatedRequest<{ cooldown_cleared: boolean }>(
    `/admin/students/${encodeURIComponent(accountID)}/devices/cooldown-resets`, "POST", locale, csrf,
  );
}
