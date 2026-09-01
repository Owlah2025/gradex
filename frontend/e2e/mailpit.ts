/**
 * Reads the transactional mail the product actually sent.
 *
 * The staff invitation link is a credential the recipient only ever receives by email. A test that
 * reads the token out of PostgreSQL and builds its own URL proves the row exists; it does not prove
 * a person who was invited can act on the invitation. So the browser journey consumes the link the
 * same way the invitee does — from the message Mailpit received over SMTP from the worker.
 *
 * The token is held in process memory and returned to the caller. It is never logged, never written
 * to a file, and never placed in a test title or an assertion message.
 */

const MAILPIT_BASE = process.env.GRADEX_E2E_MAILPIT_API || "http://127.0.0.1:8025";

type MailpitSummary = {
  ID: string;
  Subject: string;
  Created: string;
  To: Array<{ Address: string }>;
};

type MailpitMessage = {
  ID: string;
  Subject: string;
  To: Array<{ Address: string }>;
  Text: string;
  HTML: string;
};

async function readJSON<T>(path: string): Promise<T> {
  const response = await fetch(`${MAILPIT_BASE}${path}`, {
    headers: { Accept: "application/json" },
  });
  if (!response.ok) {
    throw new Error(`Mailpit request ${path} failed with HTTP ${response.status}`);
  }
  return (await response.json()) as T;
}

/**
 * Waits for exactly the message addressed to `recipient`.
 *
 * Every T8B recipient address is unique to its test, so the search can never adopt an unrelated
 * historical message: an empty result means the product has not sent the mail yet.
 */
export async function waitForMessageTo(
  recipient: string,
  timeoutMs = 30_000,
): Promise<MailpitMessage> {
  const deadline = Date.now() + timeoutMs;
  let lastCount = 0;
  while (Date.now() < deadline) {
    const search = await readJSON<{ messages: MailpitSummary[] }>(
      `/api/v1/search?query=${encodeURIComponent(`to:${recipient}`)}&limit=10`,
    );
    const matches = (search.messages ?? []).filter((message) =>
      (message.To ?? []).some((to) => to.Address.toLowerCase() === recipient.toLowerCase()),
    );
    lastCount = matches.length;
    if (matches.length > 0) {
      return await readJSON<MailpitMessage>(`/api/v1/message/${matches[0].ID}`);
    }
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  throw new Error(
    `No transactional email reached Mailpit for ${recipient} within ${timeoutMs}ms (last match count ${lastCount}).`,
  );
}

/**
 * Waits for a message to `recipient` that `matches`, newest first.
 *
 * `waitForMessageTo` takes whatever the search returns first, which is right
 * when an address receives exactly one message in its lifetime. Mailpit is a
 * shared development instance and is not reset between runs, so an address that
 * receives more than one kind of message over time — a verification code today,
 * a Course invitation an hour ago — needs the caller to say which one it means.
 * Taking the wrong one produces a failure that looks like the product not
 * sending anything.
 */
export async function waitForMessageMatching(
  recipient: string,
  matches: (message: MailpitMessage) => boolean,
  options: { timeoutMs?: number; notBefore?: Date } = {},
): Promise<MailpitMessage> {
  const timeoutMs = options.timeoutMs ?? 30_000;
  // A small tolerance for clock skew between this process and the mail server.
  const floor = options.notBefore ? options.notBefore.getTime() - 5_000 : null;
  const deadline = Date.now() + timeoutMs;
  let inspected = 0;
  while (Date.now() < deadline) {
    const search = await readJSON<{ messages: MailpitSummary[] }>(
      `/api/v1/search?query=${encodeURIComponent(`to:${recipient}`)}&limit=50`,
    );
    const summaries = (search.messages ?? [])
      .filter((message) =>
        (message.To ?? []).some((to) => to.Address.toLowerCase() === recipient.toLowerCase()),
      )
      // Newest first, explicitly. Mailpit's ordering is not part of its
      // contract, and a stale message that merely *looks* right — an expired
      // code from an earlier run against the same address — is worse than no
      // message at all: the journey types it and is correctly refused.
      .filter((message) => floor === null || Date.parse(message.Created) >= floor)
      .sort((left, right) => Date.parse(right.Created) - Date.parse(left.Created));
    inspected = summaries.length;
    for (const summary of summaries) {
      const message = await readJSON<MailpitMessage>(`/api/v1/message/${summary.ID}`);
      if (matches(message)) return message;
    }
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  throw new Error(
    `No matching transactional email reached Mailpit for ${recipient} within ${timeoutMs}ms ` +
      `(${inspected} candidate message(s) to that address were inspected).`,
  );
}

/** How many messages Mailpit currently holds for `recipient`. */
export async function messageCountFor(recipient: string): Promise<number> {
  const search = await readJSON<{ messages: MailpitSummary[] }>(
    `/api/v1/search?query=${encodeURIComponent(`to:${recipient}`)}&limit=50`,
  );
  return (search.messages ?? []).filter((message) =>
    (message.To ?? []).some((to) => to.Address.toLowerCase() === recipient.toLowerCase()),
  ).length;
}

/**
 * Extracts the user-visible action link for `pathname` from a delivered message.
 *
 * The returned string carries the one-time invitation secret in its fragment, exactly as the
 * recipient's mail client would hand it to their browser. Callers must keep it in memory only.
 */
export function actionLinkFor(message: MailpitMessage, pathname: string): string {
  const body = `${message.Text ?? ""}\n${message.HTML ?? ""}`;
  const pattern = new RegExp(`https?://[^\\s"'<>]*${pathname.replace(/\//g, "\\/")}#[^\\s"'<>]+`, "g");
  const found = body.match(pattern);
  if (!found || found.length === 0) {
    throw new Error(`The delivered message contains no ${pathname} action link.`);
  }
  // Decoded because the HTML body escapes `&` and may wrap the URL in an attribute.
  return found[0].replace(/&amp;/g, "&");
}
