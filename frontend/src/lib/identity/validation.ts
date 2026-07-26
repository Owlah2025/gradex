export const passwordMinimum = 15;
export const passwordMaximum = 128;

export function codePointLength(value: string) {
  return Array.from(value).length;
}

export function validDisplayName(value: string) {
  const name = value.trim();
  const length = codePointLength(name);
  if (length < 2 || length > 50) return false;
  const characters = Array.from(name);
  const letters = characters.filter((character) =>
    /[\p{Script=Arabic}\p{Script=Latin}]/u.test(character),
  );
  return (
    letters.length >= 2 &&
    characters.every((character) =>
      /[\p{Script=Arabic}\p{Script=Latin}\p{Mark}\p{White_Space}'’\-]/u.test(
        character,
      ),
    )
  );
}

export function validPassword(value: string) {
  const length = codePointLength(value);
  return length >= passwordMinimum && length <= passwordMaximum;
}

export function validEmail(value: string) {
  const email = value.trim();
  return (
    email.length <= 320 &&
    !/\s/.test(email) &&
    /^[^@]+@[^@]+\.[^@]+$/.test(email)
  );
}

export function takeVerificationTokenFromFragment(): string | null {
  const fragment = new URLSearchParams(window.location.hash.slice(1));
  const token = fragment.get("token");
  window.history.replaceState(
    window.history.state,
    "",
    `${window.location.pathname}${window.location.search}`,
  );
  return token;
}
