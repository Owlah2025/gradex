/**
 * The keys the player is allowed to take.
 *
 * A media player that listens on the document takes keys away from the rest of the page; one that
 * listens on itself but does not look at what was focused takes them away from its own controls.
 * Both failures look the same to a Student: a key they pressed did something other than what the
 * control under their fingers said it would.
 *
 * So this module answers one question — *may the player consume this keystroke, and as what?* — as
 * a pure function of the event, and it answers "no" for every target where a keystroke already
 * means something. The caller suppresses the browser's default only when the answer is "yes",
 * which is what keeps Space scrolling the Lesson page when the player did not claim it.
 */
export type PlayerShortcut =
  | "playPause"
  | "seekBackward"
  | "seekForward"
  | "volumeUp"
  | "volumeDown"
  | "toggleMute"
  | "toggleFullscreen"
  | "togglePictureInPicture";

/**
 * What the caller knows about the element the key was pressed on.
 *
 * Deliberately structural rather than a DOM type: the decision is about tag, role and editability,
 * and describing it this way is what lets the rule be proved without a browser.
 */
export type ShortcutTarget = {
  tagName?: string | null;
  isContentEditable?: boolean;
  role?: string | null;
  contentEditable?: string | null;
};

export type ShortcutKeyEvent = {
  key: string;
  target?: ShortcutTarget | null;
  altKey?: boolean;
  ctrlKey?: boolean;
  metaKey?: boolean;
};

/**
 * Elements that own their own keyboard.
 *
 * `INPUT`, `TEXTAREA` and `SELECT` are where typing happens; `BUTTON`, `A` and `SUMMARY` are
 * activated by Space or Enter; `OPTION` is inside an open listbox. Taking Space from a focused
 * button would mean the player's own play control could not be operated from the keyboard, and
 * taking ArrowRight from the seek slider would replace a fine-grained scrub with a ten-second jump.
 */
const INTERACTIVE_TAGS = new Set([
  "INPUT",
  "TEXTAREA",
  "SELECT",
  "BUTTON",
  "OPTION",
  "OPTGROUP",
  "A",
  "SUMMARY",
  "EMBED",
  "IFRAME",
  "OBJECT",
  "AUDIO",
  "VIDEO",
]);

/**
 * Roles that make an element behave like one of the tags above.
 *
 * A custom control built from a `div` is still a control. The list covers the widget roles whose
 * authored keyboard interaction includes the keys this player wants.
 */
const INTERACTIVE_ROLES = new Set([
  "button",
  "checkbox",
  "combobox",
  "link",
  "listbox",
  "menu",
  "menubar",
  "menuitem",
  "menuitemcheckbox",
  "menuitemradio",
  "option",
  "radio",
  "searchbox",
  "slider",
  "spinbutton",
  "switch",
  "tab",
  "textbox",
  "treeitem",
]);

/** Whether a keystroke on this target already means something the player must not override. */
export function targetOwnsKeyboard(target: ShortcutTarget | null | undefined): boolean {
  if (!target) return false;
  if (target.isContentEditable === true) return true;
  const contentEditable = target.contentEditable;
  if (typeof contentEditable === "string" && contentEditable !== "false" && contentEditable !== "inherit") return true;
  const tagName = typeof target.tagName === "string" ? target.tagName.toUpperCase() : "";
  if (INTERACTIVE_TAGS.has(tagName)) return true;
  const role = typeof target.role === "string" ? target.role.toLowerCase() : "";
  return INTERACTIVE_ROLES.has(role);
}

/**
 * The shortcut a key names, before any question of whether it may be taken.
 *
 * Letters are matched case-insensitively so a shortcut still works with Caps Lock on or Shift
 * held. `P` is not bound: a bare `P` is close enough to the print gesture that Students press it
 * by accident, and `I` is the key the Picture-in-Picture control advertises.
 */
function shortcutForKey(key: string): PlayerShortcut | null {
  switch (key) {
    case " ":
    case "Spacebar":
      return "playPause";
    case "ArrowLeft":
      return "seekBackward";
    case "ArrowRight":
      return "seekForward";
    case "ArrowUp":
      return "volumeUp";
    case "ArrowDown":
      return "volumeDown";
    default:
      break;
  }
  switch (key.toLowerCase()) {
    case "k":
      return "playPause";
    case "j":
      return "seekBackward";
    case "l":
      return "seekForward";
    case "m":
      return "toggleMute";
    case "f":
      return "toggleFullscreen";
    case "i":
      return "togglePictureInPicture";
    default:
      return null;
  }
}

/**
 * The shortcut this event may be consumed as, or `null` when the player must keep its hands off.
 *
 * A modifier held means the keystroke belongs to the browser or the operating system — `Ctrl+L` is
 * the address bar, `Cmd+M` minimises the window — so no combination is ever claimed. Shift is not
 * in that list because it only changes the letter.
 */
export function playerShortcutFor(event: ShortcutKeyEvent): PlayerShortcut | null {
  if (event.altKey === true || event.ctrlKey === true || event.metaKey === true) return null;
  if (targetOwnsKeyboard(event.target)) return null;
  return shortcutForKey(event.key);
}

/** The step a single volume shortcut moves, as a fraction of full volume. */
export const VOLUME_STEP = 0.05;
