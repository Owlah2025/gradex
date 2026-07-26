"use client";

import Link from "next/link";
import { LockKeyhole } from "lucide-react";
import { Logo } from "@/components/brand/logo";
import { LanguageToggle } from "@/components/common/language-toggle";
import { useLocale } from "@/lib/i18n/locale-provider";

export function AuthShell({
  title,
  intro,
  children,
}: {
  title: string;
  intro: string;
  children: React.ReactNode;
}) {
  const { t } = useLocale();
  return (
    <main id="main" className="min-h-dvh bg-background lg:grid lg:grid-cols-[minmax(0,1.05fr)_minmax(360px,0.72fr)]">
      <section className="flex min-h-dvh flex-col px-5 py-5 sm:px-8 lg:px-12 lg:py-8 xl:px-20">
        <header className="flex items-center justify-between gap-4">
          <Logo ariaLabel={t.meta.logoHomeAria} />
          <LanguageToggle />
        </header>

        <div className="mx-auto flex w-full max-w-[560px] flex-1 flex-col justify-center py-12 sm:py-16">
          <p className="mb-3 font-mono text-xs font-semibold uppercase tracking-[0.2em] text-primary">
            {t.auth.shell.eyebrow}
          </p>
          <h1 className="max-w-xl text-3xl font-extrabold tracking-tight sm:text-4xl">
            {title}
          </h1>
          <p className="mt-4 max-w-lg text-base leading-7 text-muted-foreground">
            {intro}
          </p>
          <div className="mt-8">{children}</div>
        </div>

        <footer className="flex flex-col items-start justify-between gap-3 border-t pt-5 text-sm text-muted-foreground sm:flex-row sm:items-center sm:gap-4">
          <Link className="font-semibold hover:text-foreground" href="/">
            {t.auth.common.backHome}
          </Link>
          <span className="flex items-center gap-2">
            <LockKeyhole className="size-4" aria-hidden />
            {t.auth.shell.privacy}
          </span>
        </footer>
      </section>

      <aside className="relative hidden overflow-hidden bg-gx-navy text-white lg:flex lg:flex-col lg:justify-between lg:p-12 xl:p-16">
        <div
          className="absolute inset-0 opacity-[0.08]"
          aria-hidden
          style={{
            backgroundImage:
              "repeating-linear-gradient(to bottom, transparent 0, transparent 47px, white 48px)",
          }}
        />
        <div className="relative">
          <span className="inline-flex rounded-pill border border-white/20 px-4 py-2 font-mono text-xs uppercase tracking-[0.18em] text-gx-blue-200">
            Gradex · Kuwait
          </span>
          <h2 className="mt-8 max-w-md text-4xl font-extrabold leading-tight text-white xl:text-5xl">
            {t.auth.shell.sideTitle}
          </h2>
          <p className="mt-5 max-w-md text-lg leading-8 text-white/70">
            {t.auth.shell.sideBody}
          </p>
        </div>

        <ol className="relative mt-12 space-y-0">
          {t.auth.shell.steps.map((step, index) => (
            <li key={step} className="relative flex min-h-20 items-start gap-5">
              {index < t.auth.shell.steps.length - 1 ? (
                <span
                  className="absolute start-[15px] top-8 h-12 w-px bg-white/25"
                  aria-hidden
                />
              ) : null}
              <span
                className={`relative z-10 grid size-8 shrink-0 place-items-center rounded-full border font-mono text-xs font-bold ${
                  index === 0
                    ? "border-gx-orange bg-gx-orange text-gx-navy"
                    : "border-white/35 bg-gx-navy text-white/70"
                }`}
              >
                {index + 1}
              </span>
              <span className="pt-1 font-display text-lg font-bold">{step}</span>
            </li>
          ))}
        </ol>
      </aside>
    </main>
  );
}
