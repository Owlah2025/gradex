import { expect, type BrowserContext, type Page } from "@playwright/test";

export type LoginDeviceTrust = {
  state?: string;
  admission?: string;
  challenge?: { challenge_id: string };
};

/** Login and legacy adoption must admit the browser without a device challenge. */
export function completeDeviceTrustIfRequired(
  _page: Page,
  _email: string,
  deviceTrust: LoginDeviceTrust | undefined,
  _requestedAt: Date,
): void {
  expect(deviceTrust?.state).toBe("TRUSTED");
  expect(deviceTrust?.challenge).toBeUndefined();
}

/** True when this context already holds a device credential. */
export async function hasDeviceCredential(context: BrowserContext): Promise<boolean> {
  const cookies = await context.cookies();
  return cookies.some((cookie) => cookie.name === "__Host-gradex_device");
}
