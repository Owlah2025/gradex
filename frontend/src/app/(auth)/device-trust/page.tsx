"use client";

import { AuthShell } from "@/components/auth/auth-shell";
import { DeviceTrustForm } from "@/components/auth/device-trust-form";
import { useLocale } from "@/lib/i18n/locale-provider";

export default function DeviceTrustPage() {
  const { t } = useLocale();
  return (
    <AuthShell title={t.devices.trustTitle} intro={t.devices.intro} activeStep={2}>
      <DeviceTrustForm />
    </AuthShell>
  );
}
