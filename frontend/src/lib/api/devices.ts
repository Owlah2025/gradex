import { authenticatedRequest } from "./http";

/**
 * The Student trusted-device surface.
 *
 * Nothing here is a secret. The device credential itself lives in an HttpOnly
 * `__Host-` cookie that JavaScript can never read, and every call below is
 * authorized by the ordinary session cookie — the device credential only tells
 * the server *which* of the Student's browsers is asking. `challenge_id` names
 * a challenge and authenticates nobody: presenting it without the emailed code
 * is exactly as useful as presenting nothing.
 */

export type DeviceTrustState =
  | "NOT_APPLICABLE"
  | "PENDING_DEVICE_TRUST"
  | "TRUSTED"
  | "LEGACY_UNBOUND";

export type DeviceAdmission = "TRUSTED" | "OTP_REQUIRED" | "LIMIT_REACHED";

export type DeviceChallenge = {
  challenge_id: string;
  masked_email: string;
  expires_at: string;
  resend_available_at: string;
};

export type SessionDeviceTrust = {
  state: DeviceTrustState;
  admission?: DeviceAdmission;
  challenge?: DeviceChallenge;
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

export type DeviceTrustCompleted = {
  state: "TRUSTED";
  replaced_device: boolean;
  revoked_sessions: number;
  device_registered: boolean;
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

/**
 * Completes a device-trust challenge.
 *
 * The device being trusted is not a parameter, and that is deliberate: the
 * server reads it from the challenge and then requires this browser to prove it
 * holds that device's credential. A caller cannot aim a code at a browser it
 * was not mailed for.
 */
export function trustDevice(
  challengeID: string,
  code: string,
  locale: "ar" | "en",
  csrf: string,
  replaceDeviceID?: string,
) {
  return authenticatedRequest<DeviceTrustCompleted>(
    "/me/devices/trust",
    "POST",
    locale,
    csrf,
    { code, replace_device_id: replaceDeviceID ?? "" },
    { "X-Gradex-Device-Challenge": challengeID },
  ) as Promise<DeviceTrustCompleted>;
}

export function resendDeviceCode(
  challengeID: string,
  locale: "ar" | "en",
  csrf: string,
) {
  return authenticatedRequest<{ challenge: DeviceChallenge }>(
    "/me/devices/trust/resend",
    "POST",
    locale,
    csrf,
    undefined,
    { "X-Gradex-Device-Challenge": challengeID },
  ) as Promise<{ challenge: DeviceChallenge }>;
}

/**
 * Binds a session created before device policy existed to this browser.
 *
 * Answers `TRUSTED` when the browser already holds a live trusted record — the
 * Student proved this exact browser once and is not asked again — and otherwise
 * returns the ordinary challenge.
 */
export function adoptDevice(locale: "ar" | "en", csrf: string) {
  return authenticatedRequest<SessionDeviceTrust>(
    "/me/devices/adopt",
    "POST",
    locale,
    csrf,
  ) as Promise<SessionDeviceTrust>;
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
