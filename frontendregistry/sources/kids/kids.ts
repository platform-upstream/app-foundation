// Kid profiles in a game without a look (app-foundation registry module `kids`): the copy (de, en),
// the takeover suggestion and the photo shrink. Registry-owned: do not edit in an application.
// The React parts are in Kids.tsx.

export type KidsLanguage = 'de' | 'en';

const DE = {
  add: 'Kind',
  addLabel: 'Kind anlegen',
  editKid: '{name} bearbeiten',
  edit: 'Bearbeiten',
  done: 'Fertig',
  guestNote: 'Ohne Anmeldung: Alles bleibt auf diesem Gerät.',
  signIn: 'Anmelden',
  signOut: 'Abmelden',
  privacy: 'Datenschutz',
  adminOnly: 'Kinder legt an, wer die Familie verwaltet.',
  waitingOne: '1 Kind von diesem Gerät wartet noch',
  waiting: '{n} Kinder von diesem Gerät warten noch',
  adopt: 'Übernehmen',
  name: 'Name',
  namePlaceholder: 'z. B. Mia',
  nameLocked: 'Den Namen ändert, wer die Familie verwaltet.',
  photo: 'Foto',
  optional: '(freiwillig)',
  pickPhoto: 'Foto wählen',
  otherPhoto: 'Anderes Foto',
  noPhoto: 'Kein Foto',
  photoUnreadable: 'Das Foto lässt sich nicht öffnen.',
  save: 'Speichern',
  saving: 'Speichert …',
  cancel: 'Abbrechen',
  remove: 'Kind entfernen',
  reallyRemove: '{name} wirklich löschen',
  savedNoPhoto: '{name} ist gespeichert, das Foto nicht: {error}',
  failed: 'Das hat gerade nicht geklappt.',
  adoptLead: 'Diese Kinder haben auf diesem Gerät ohne Anmeldung gespielt. Zu wem soll, was sie gemacht haben?',
  adoptTo: '{name} übernehmen zu',
  toKid: 'zu {name}',
  asNew: 'als neues Kind',
  notYet: 'noch nicht',
  later: 'Später',
  dropAll: 'Alles löschen',
  reallyDropAll: 'Wirklich alles löschen',
  replace: 'Das ersetzt, was {names} hier schon gemacht hat.',
  replaceOk: 'Trotzdem übernehmen',
  deleteAccount: 'Konto löschen',
  deleteAll: 'Alles löschen',
  deleted: 'Gelöscht.',
  contact: 'Die Anmeldung und die Kinderprofile löscht der Betreiber auf Anfrage: {contact}.',
  contactNone: 'Die Anmeldung und die Kinderprofile löscht der Betreiber auf Anfrage.',
};

export type KidsText = typeof DE;

const EN: KidsText = {
  add: 'Kid',
  addLabel: 'Add a kid',
  editKid: 'Edit {name}',
  edit: 'Edit',
  done: 'Done',
  guestNote: 'Not signed in: everything stays on this device.',
  signIn: 'Sign in',
  signOut: 'Sign out',
  privacy: 'Privacy',
  adminOnly: 'Kids are added by whoever runs the family.',
  waitingOne: '1 kid from this device is waiting',
  waiting: '{n} kids from this device are waiting',
  adopt: 'Take over',
  name: 'Name',
  namePlaceholder: 'e.g. Mia',
  nameLocked: 'Whoever runs the family changes the name.',
  photo: 'Photo',
  optional: '(optional)',
  pickPhoto: 'Choose a photo',
  otherPhoto: 'Another photo',
  noPhoto: 'No photo',
  photoUnreadable: 'This photo cannot be opened.',
  save: 'Save',
  saving: 'Saving …',
  cancel: 'Cancel',
  remove: 'Remove kid',
  reallyRemove: 'Really delete {name}',
  savedNoPhoto: '{name} is saved, the photo is not: {error}',
  failed: 'That did not work just now.',
  adoptLead: 'These kids played on this device without signing in. Who should get what they made?',
  adoptTo: 'Take {name} over to',
  toKid: 'to {name}',
  asNew: 'as a new kid',
  notYet: 'not yet',
  later: 'Later',
  dropAll: 'Delete everything',
  reallyDropAll: 'Really delete everything',
  replace: 'This replaces what {names} already made here.',
  replaceOk: 'Take over anyway',
  deleteAccount: 'Delete account',
  deleteAll: 'Delete everything',
  deleted: 'Deleted.',
  contact: 'The operator deletes the sign-in and the kid profiles on request: {contact}.',
  contactNone: 'The operator deletes the sign-in and the kid profiles on request.',
};

/** The module's copy in a language, with the app's overrides (e.g. a game's own adoptLead). */
export function kidsText(lang: KidsLanguage = 'de', overrides: Partial<KidsText> = {}): KidsText {
  return { ...(lang === 'en' ? EN : DE), ...overrides };
}

/** Fills {name}-style placeholders. */
export function fill(template: string, vars: Record<string, string | number>): string {
  return template.replace(/\{(\w+)\}/g, (all, key: string) => (key in vars ? String(vars[key]) : all));
}

/** The two languages' keys, for tests. */
export const kidsLanguages: Record<KidsLanguage, KidsText> = { de: DE, en: EN };

/** Adopt targets besides a kid id. */
export const AS_NEW = '+new';
export const NOT_YET = '';

const fold = (s: string) => s.trim().toLocaleLowerCase().normalize('NFKD').replace(/\p{M}/gu, '');
const firstName = (s: string) => fold(s).split(/\s+/)[0] ?? '';

/**
 * Where a guest kid goes by default after a sign-in: the kid with the same name, else the one with
 * the same first name, else a new kid (when the adult may add kids), else the first kid, else
 * "not yet".
 */
export function suggestTarget(guestName: string, kids: readonly { id: string; name: string }[], canAdd: boolean): string {
  const same = kids.find((k) => fold(k.name) === fold(guestName));
  if (same) return same.id;
  const first = firstName(guestName);
  const sameFirst = first ? kids.find((k) => firstName(k.name) === first) : undefined;
  if (sameFirst) return sameFirst.id;
  if (canAdd) return AS_NEW;
  return kids[0]?.id ?? NOT_YET;
}

/** How a privacy contact is linked: an https URL as is, an e-mail address as mailto:, else none. */
export function contactHref(contact: string): string | undefined {
  const c = contact.trim();
  if (c.startsWith('https://')) return c;
  if (/^[^\s@]+@[^\s@]+$/.test(c)) return `mailto:${c}`;
  return undefined;
}

/** The size every game shows and directory keeps: the longer side at most 512 px. */
export const PHOTO_SIZE = 512;

/** The scaled size of a picture: the longer side at most max, never upscaled, never 0. */
export function fitSize(width: number, height: number, max = PHOTO_SIZE): { width: number; height: number } {
  const scale = Math.min(1, max / Math.max(width, height, 1));
  return { width: Math.max(1, Math.round(width * scale)), height: Math.max(1, Math.round(height * scale)) };
}

/**
 * Reads a picture the adult took or chose and returns a JPEG of at most 512 px (EXIF orientation
 * applied). Kept on the device for a guest, sent to directory through the app when signed in.
 */
export async function shrinkPhoto(file: Blob, max = PHOTO_SIZE): Promise<Blob> {
  const bitmap = await createImageBitmap(file, { imageOrientation: 'from-image' }).catch(() => null);
  const source: CanvasImageSource & { width: number; height: number } = bitmap ?? (await loadImage(file));
  const { width, height } = fitSize(source.width, source.height, max);
  const canvas = document.createElement('canvas');
  canvas.width = width;
  canvas.height = height;
  const ctx = canvas.getContext('2d');
  if (!ctx) throw new Error('no canvas');
  ctx.drawImage(source, 0, 0, width, height);
  bitmap?.close();
  return new Promise((resolve, reject) => {
    canvas.toBlob((b) => (b ? resolve(b) : reject(new Error('no JPEG'))), 'image/jpeg', 0.82);
  });
}

function loadImage(file: Blob): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const url = URL.createObjectURL(file);
    const img = new Image();
    img.onload = () => { URL.revokeObjectURL(url); resolve(img); };
    img.onerror = () => { URL.revokeObjectURL(url); reject(new Error('unreadable')); };
    img.src = url;
  });
}

/** A Blob as a data: URL (a guest's photo is kept in localStorage this way). */
export function blobToDataURL(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const r = new FileReader();
    r.onload = () => resolve(String(r.result));
    r.onerror = () => reject(r.error ?? new Error('unreadable'));
    r.readAsDataURL(blob);
  });
}
