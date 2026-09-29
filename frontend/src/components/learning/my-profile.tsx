"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { getMyProfile, updateMyProfile, type MyProfile } from "@/lib/api/account-profile";
import { currentCSRFToken } from "@/lib/identity/session";
import { useLocale } from "@/lib/i18n/locale-provider";
import { ErrorState } from "@/components/common/error-state";
import { LoadingState } from "@/components/common/loading-state";
import { StatusBadge } from "@/components/common/status-badge";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";

export function MyProfile() {
  const { locale, t } = useLocale();
  const copy = t.account;
  const router = useRouter();
  const [profile, setProfile] = useState<MyProfile | null>(null);
  const [displayName, setDisplayName] = useState("");
  const [nextLocale, setNextLocale] = useState<"ar" | "en">(locale);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    getMyProfile(locale)
      .then((next) => {
        if (cancelled) return;
        setProfile(next);
        setDisplayName(next.display_name);
        setNextLocale(next.locale);
      })
      .catch((cause: unknown) => {
        if (!cancelled) setError(cause instanceof Error ? cause.message : copy.loadFailed);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [copy.loadFailed, locale]);

  const save = async () => {
    const csrf = currentCSRFToken();
    if (!csrf) {
      setError(locale === "ar" ? "انتهت الجلسة. سجّل الدخول مرة أخرى." : "Your session ended. Sign in again.");
      return;
    }
    setSaving(true);
    setError(null);
    setMessage(null);
    try {
      const updated = await updateMyProfile({
        locale,
        csrf,
        displayName,
        nextLocale,
      });
      setProfile(updated);
      setMessage(copy.saved);
      if (nextLocale !== locale) router.push("/" + nextLocale + "/learn/profile");
    } catch (cause: unknown) {
      setError(cause instanceof Error ? cause.message : copy.loadFailed);
    } finally {
      setSaving(false);
    }
  };

  if (loading) return <LoadingState label={copy.loading} />;
  if (error && !profile) return <ErrorState title={copy.loadFailed} detail={error} retryLabel={copy.retry} onRetry={() => window.location.reload()} />;
  if (!profile) return null;

  const academic = profile.academic_profile;

  return (
    <div className="space-y-6" data-testid="my-profile">
      <header>
        <p className="text-sm font-semibold text-primary">{copy.identityTitle}</p>
        <h1 className="mt-2 font-display text-3xl font-bold text-foreground sm:text-4xl">{copy.profileTitle}</h1>
        <p className="mt-2 max-w-2xl text-sm leading-6 text-muted-foreground">{copy.profileDescription}</p>
      </header>
      {error ? <Alert tone="error" title={copy.loadFailed}>{error}</Alert> : null}
      {message ? <Alert tone="success" title={message} /> : null}

      <Card>
        <CardHeader><CardTitle>{copy.identityTitle}</CardTitle></CardHeader>
        <CardContent className="grid gap-5 md:grid-cols-2">
          <Field label={copy.displayName} htmlFor="my-profile-display-name">
            <Input id="my-profile-display-name" value={displayName} onChange={(event) => setDisplayName(event.target.value)} maxLength={50} />
          </Field>
          <Field label={copy.email} htmlFor="my-profile-email">
            <Input id="my-profile-email" value={profile.email} readOnly aria-describedby="my-profile-email-help" />
            <p id="my-profile-email-help" className="mt-2 text-xs leading-5 text-muted-foreground">{copy.emailReadOnly} {copy.emailSupport}</p>
          </Field>
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>{copy.languageTitle}</CardTitle></CardHeader>
        <CardContent className="grid gap-5 sm:grid-cols-[minmax(0,20rem)_auto] sm:items-end">
          <Field label={copy.languageTitle} htmlFor="my-profile-language">
            <Select id="my-profile-language" value={nextLocale} onChange={(event) => setNextLocale(event.target.value as "ar" | "en")}>
              <option value="ar">{copy.arabic}</option>
              <option value="en">{copy.english}</option>
            </Select>
          </Field>
          <Button type="button" onClick={() => void save()} disabled={saving}>{saving ? copy.saving : copy.save}</Button>
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>{copy.academicTitle}</CardTitle></CardHeader>
        <CardContent>
          {academic ? (
            <dl className="grid gap-4 sm:grid-cols-2">
              {academic.institution ? <div><dt className="text-xs font-semibold text-muted-foreground">{copy.institution}</dt><dd className="mt-1 font-semibold text-foreground"><bdi>{academic.institution}</bdi></dd></div> : null}
              {academic.college_unit ? <div><dt className="text-xs font-semibold text-muted-foreground">{copy.collegeUnit}</dt><dd className="mt-1 font-semibold text-foreground"><bdi>{academic.college_unit}</bdi></dd></div> : null}
              {academic.program ? <div><dt className="text-xs font-semibold text-muted-foreground">{copy.program}</dt><dd className="mt-1 font-semibold text-foreground"><bdi>{academic.program}</bdi></dd></div> : null}
              {academic.level ? <div><dt className="text-xs font-semibold text-muted-foreground">{copy.level}</dt><dd className="mt-1 font-semibold text-foreground">{academic.level}</dd></div> : null}
            </dl>
          ) : <p className="text-sm text-muted-foreground">{copy.academicEmpty}</p>}
          <Button className="mt-5" asChild variant="outline"><Link href={"/" + locale + "/learn/academic-profile"}>{copy.editAcademic}</Link></Button>
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>{copy.statusTitle}</CardTitle></CardHeader>
        <CardContent className="flex flex-wrap items-center justify-between gap-4">
          <StatusBadge label={copy.statusLabels[profile.status]} />
          <Button asChild variant="outline">
            <Link href={"/password-change?returnTo=" + encodeURIComponent("/" + locale + "/learn/profile")}>{copy.securityLink}: {copy.changePassword}</Link>
          </Button>
        </CardContent>
      </Card>
    </div>
  );
}
