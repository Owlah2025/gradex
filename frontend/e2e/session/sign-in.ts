import type { BrowserContext, Page } from "@playwright/test";
import { installIssuedSession, issueRotatingSession } from "../rotating-students";
import type { SeededPrincipal } from "./principals";

export async function signInWithSeededSession(
  context: BrowserContext,
  principal: SeededPrincipal,
  locale: "ar" | "en" = "en",
): Promise<Page> {
  await context.addInitScript((selectedLocale) => {
    window.localStorage.setItem("gradex.locale", selectedLocale);
  }, locale);
  await installIssuedSession(context, issueRotatingSession(principal));
  return context.newPage();
}
