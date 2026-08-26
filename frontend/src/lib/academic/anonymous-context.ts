/**
 * The academic context an anonymous visitor chooses before they have an account.
 *
 * This is a *browsing preference*, not a profile. Nothing here is an authority over anything: it
 * narrows a public read, it is never sent to the server, and it never becomes
 * `PUT /me/academic-profile` state. The authenticated academic profile is a separate concern with a
 * separate contract, and the two are deliberately not joined — see `resolveAcademicContext` and the
 * note on the identifier gap at the bottom of this file.
 *
 * ## Why `localStorage`
 *
 * The catalogue already makes the URL the single owner of the *filter* selection, which is right:
 * refresh, a shared link, and browser back/forward then all behave the same way. But a URL cannot
 * remember anything across a visit to the landing page, a Course, or a second session, and that is
 * exactly the product requirement — a Student should not have to name their university again every
 * time they come back.
 *
 * So the two hold different things:
 *
 *  - the **URL** owns what the catalogue is showing right now, including level and Subject, and
 *    stays the shareable, back/forward-correct surface it already was;
 *  - **`localStorage`** owns the visitor's durable academic identity — institution and program —
 *    and seeds the URL when the catalogue is opened without one.
 *
 * `sessionStorage` was rejected because it forgets on every new tab, which is most of what "coming
 * back to Gradex" looks like. React state alone was rejected for the same reason a URL is not
 * enough: it does not survive a reload.
 *
 * Level and Subject are deliberately *not* persisted. A level here is "level N **of this study
 * plan**", so its meaning changes with the program and it is re-chosen every term; a Subject is a
 * search, not an identity. Institution and program are the two facts that stay true between visits,
 * and they are also exactly the two an authenticated academic profile can later confirm.
 *
 * ## Failing safely
 *
 * Everything read back out of the browser is treated as hostile: another tab, an older release, a
 * hand-edited value, or a quota failure can all produce something that is not what was written.
 * `parseAcademicContext` returns `null` for anything it cannot fully vouch for rather than
 * partially trusting it, and every accessor swallows the storage exception a private-mode browser
 * throws. A corrupt value must degrade to "no context", never to a crash during hydration.
 */

export const ACADEMIC_CONTEXT_STORAGE_KEY = "gradex.academic-context";

/**
 * Bumped whenever the stored shape changes meaning. A record written by any other version is
 * discarded rather than migrated: this is a re-askable preference, so dropping it costs the visitor
 * two selects and guarantees no half-understood record is ever acted on.
 */
export const ACADEMIC_CONTEXT_VERSION = 1;

/**
 * Cached localized names, for presentation only.
 *
 * These exist so the context bar can name the university on first paint instead of flashing a slug
 * or an empty strip while the option lists load. They are **never** identity: nothing matches on
 * them, nothing is looked up by them, and a stale one is corrected the moment the real options
 * arrive. Both languages are cached together, because the identity must survive a locale switch and
 * a single-language cache would have to be thrown away at exactly that moment.
 */
export type AcademicContextNames = {
  institutionAr: string;
  institutionEn: string;
  programAr: string;
  programEn: string;
};

export type AnonymousAcademicContext = {
  /** The public institution slug. This, with `programSlug`, is the whole identity. */
  institutionSlug: string;
  /** The public program slug, or "" for a visitor who named only their university. */
  programSlug: string;
  /** Non-authoritative display cache. Safe to be empty, stale, or ignored. */
  names: AcademicContextNames;
};

type StoredRecord = {
  version: number;
  institutionSlug: string;
  programSlug: string;
  names: AcademicContextNames;
};

const EMPTY_NAMES: AcademicContextNames = {
  institutionAr: "",
  institutionEn: "",
  programAr: "",
  programEn: "",
};

function readString(source: Record<string, unknown>, key: string): string {
  const value = source[key];
  return typeof value === "string" ? value.trim() : "";
}

function readNames(value: unknown): AcademicContextNames {
  if (!value || typeof value !== "object") return { ...EMPTY_NAMES };
  const source = value as Record<string, unknown>;
  return {
    institutionAr: readString(source, "institutionAr"),
    institutionEn: readString(source, "institutionEn"),
    programAr: readString(source, "programAr"),
    programEn: readString(source, "programEn"),
  };
}

/**
 * Turns whatever the browser handed back into a context, or into `null`.
 *
 * A record is only honoured when it carries the current version *and* an institution slug. A
 * program without an institution is not a partial context that can be repaired — the program option
 * list is fetched per institution, so there is nothing to validate it against — and it is dropped
 * whole.
 */
export function parseAcademicContext(
  raw: string | null | undefined,
): AnonymousAcademicContext | null {
  if (typeof raw !== "string" || raw === "") return null;
  let decoded: unknown;
  try {
    decoded = JSON.parse(raw);
  } catch {
    return null;
  }
  if (!decoded || typeof decoded !== "object" || Array.isArray(decoded)) return null;
  const record = decoded as Record<string, unknown>;
  if (record.version !== ACADEMIC_CONTEXT_VERSION) return null;

  const institutionSlug = readString(record, "institutionSlug");
  if (institutionSlug === "") return null;

  return {
    institutionSlug,
    programSlug: readString(record, "programSlug"),
    names: readNames(record.names),
  };
}

/** The exact bytes written to the browser. Kept next to the parser so the two cannot drift. */
export function serializeAcademicContext(
  context: AnonymousAcademicContext,
): string {
  const record: StoredRecord = {
    version: ACADEMIC_CONTEXT_VERSION,
    institutionSlug: context.institutionSlug,
    programSlug: context.programSlug,
    names: context.names,
  };
  return JSON.stringify(record);
}

/**
 * The subset of `Storage` this module uses, so the whole thing is testable without a browser and
 * cannot reach for anything wider than it needs.
 */
export type ContextStorage = Pick<Storage, "getItem" | "setItem" | "removeItem">;

/**
 * The browser's `localStorage`, or `null` on the server and wherever it is unavailable.
 *
 * Accessing `window.localStorage` throws outright in some privacy configurations, so even the
 * lookup is guarded. Returning `null` rather than a stub keeps "there is no storage here" a state
 * every caller has to handle, which is also the server-render state.
 */
export function browserContextStorage(): ContextStorage | null {
  if (typeof window === "undefined") return null;
  try {
    return window.localStorage;
  } catch {
    return null;
  }
}

export function readAcademicContext(
  storage: ContextStorage | null,
): AnonymousAcademicContext | null {
  if (!storage) return null;
  try {
    return parseAcademicContext(storage.getItem(ACADEMIC_CONTEXT_STORAGE_KEY));
  } catch {
    return null;
  }
}

/** Writing a context with no institution clears the record instead of storing an empty one. */
export function writeAcademicContext(
  storage: ContextStorage | null,
  context: AnonymousAcademicContext | null,
): void {
  if (!storage) return;
  try {
    if (context === null || context.institutionSlug === "") {
      storage.removeItem(ACADEMIC_CONTEXT_STORAGE_KEY);
      return;
    }
    storage.setItem(
      ACADEMIC_CONTEXT_STORAGE_KEY,
      serializeAcademicContext(context),
    );
  } catch {
    // A full or refusing storage must not break browsing. The visitor keeps the
    // context for this page load and is simply asked again next visit.
  }
}

export function clearAcademicContext(storage: ContextStorage | null): void {
  writeAcademicContext(storage, null);
}

export function academicContext(
  institutionSlug: string,
  programSlug: string,
  names: Partial<AcademicContextNames> = {},
): AnonymousAcademicContext {
  return {
    institutionSlug,
    programSlug: institutionSlug === "" ? "" : programSlug,
    names: { ...EMPTY_NAMES, ...names },
  };
}

export function emptyAcademicContextNames(): AcademicContextNames {
  return { ...EMPTY_NAMES };
}

/** The localized names to show, in the visitor's language. Never used to identify anything. */
export function academicContextNames(
  context: AnonymousAcademicContext,
  locale: "ar" | "en",
): { institution: string; program: string } {
  return locale === "ar"
    ? { institution: context.names.institutionAr, program: context.names.programAr }
    : { institution: context.names.institutionEn, program: context.names.programEn };
}

/** Whether two contexts name the same thing. Compares identity only — never the display cache. */
export function sameAcademicContext(
  left: AnonymousAcademicContext | null,
  right: AnonymousAcademicContext | null,
): boolean {
  if (left === null || right === null) return left === right;
  return (
    left.institutionSlug === right.institutionSlug &&
    left.programSlug === right.programSlug
  );
}

/**
 * Drops the parts of a stored context the catalogue can no longer offer.
 *
 * Only the invalid portion goes. A university that has been retired invalidates the program beneath
 * it as well, because that program was chosen from *its* list; a program that has disappeared under
 * a university that still exists leaves the university selected, so the visitor re-picks one field
 * rather than starting over.
 *
 * `null` for either list means "not loaded yet", which is not evidence of anything and leaves the
 * context untouched. That distinction is the whole point: treating an in-flight request as an empty
 * catalogue would wipe a valid context on every slow connection.
 */
export function validateAcademicContext(
  context: AnonymousAcademicContext | null,
  knownInstitutionSlugs: readonly string[] | null,
  knownProgramSlugs: readonly string[] | null,
): AnonymousAcademicContext | null {
  if (context === null) return null;
  if (knownInstitutionSlugs !== null) {
    if (!knownInstitutionSlugs.includes(context.institutionSlug)) return null;
  }
  if (
    context.programSlug !== "" &&
    knownProgramSlugs !== null &&
    !knownProgramSlugs.includes(context.programSlug)
  ) {
    return { ...context, programSlug: "", names: { ...context.names, programAr: "", programEn: "" } };
  }
  return context;
}

/**
 * Which academic context the product should act on.
 *
 * A Student who has actually completed their academic profile has told the server who they are, and
 * that answer outranks a preference some browser is holding: it is the one the account owns, it
 * follows them between devices, and it is the one they can edit. A profile that is `NOT_STARTED` or
 * `SKIPPED` asserts nothing, so the browsing preference is the only thing anyone has said and it
 * stands.
 *
 * The anonymous record is never deleted when the profile wins. The visitor may sign out, and
 * throwing away a preference they never asked to lose would be the same mistake in the other
 * direction. It is outranked, not overwritten — and it is never written *to* the profile either:
 * see the gap note below.
 */
export type ResolvedAcademicSource = "profile" | "anonymous" | "none";

export function resolveAcademicSource(input: {
  anonymous: AnonymousAcademicContext | null;
  profileSetupState: "NOT_STARTED" | "SKIPPED" | "COMPLETED" | null;
}): ResolvedAcademicSource {
  if (input.profileSetupState === "COMPLETED") return "profile";
  return input.anonymous === null ? "none" : "anonymous";
}

/**
 * ## The identifier gap, stated once
 *
 * The public option endpoints (`/catalog/academic-options/...`) speak in **slugs**.
 * `PUT /me/academic-profile` speaks in **UUIDs**, and the authenticated option endpoints
 * (`/me/academic-options/...`) return UUIDs with no slug on them. A saved profile carries
 * `program_slug` — but that is the UUID→slug direction, produced by the server *after* a profile
 * exists, and there is no endpoint anywhere that takes a public slug and returns the identifier
 * `PUT /me/academic-profile` requires.
 *
 * So no automatic binding is possible, and none is attempted. The one bridge that would "work"
 * without a contract — comparing `name_ar`/`name_en` between the two lists — is forbidden: display
 * names are localized, editable prose that two different programs can legitimately share, and
 * matching on them would silently write the wrong program onto a real account.
 *
 * What happens instead is in `AcademicProfileForm`: the anonymous context is shown as *guidance*
 * beside the authenticated options, and the Student confirms it by choosing from the real list.
 * Closing this properly needs a server contract (a slug on the authenticated options, or a
 * slug-accepting write) and is out of scope here.
 */
