// Kid profiles in a game (app-foundation registry module `kids`). Registry-owned: do not edit in
// an application. One UX in every game: kid tiles with photos and "+ Kind" last, the guest note
// under them, the banner for guest kids waiting after a sign-in, the kid form (name, photo, then
// the game's own fields), the takeover and the account actions. No look: the classes below sit in
// @layer foundation for layout only; the game paints them and passes its button classes.
// Content only: the game puts the form and the takeover into its own sheet or modal.
import { createContext, useContext, useEffect, useMemo, useRef, useState, type CSSProperties, type ReactNode } from 'react';
import { AS_NEW, NOT_YET, contactHref, fill, kidsText, shrinkPhoto, suggestTarget, type KidsLanguage, type KidsText } from './kids.js';

export { AS_NEW, NOT_YET, blobToDataURL, kidsText, shrinkPhoto, suggestTarget, type KidsLanguage, type KidsText } from './kids.js';

export interface KidsSettings {
  lang?: KidsLanguage;
  /** The game's own wording (e.g. adoptLead naming castles). */
  text?: Partial<KidsText>;
  /** The game's button classes; every button gets `button`, plus `primary` or `danger`. */
  button?: string;
  primary?: string;
  danger?: string;
  /** For quiet text buttons (Anmelden in the note, Bearbeiten). Default: `button`. */
  link?: string;
  /** Starts the sign-in (the game's own: it remembers the device is on the way to a login). */
  onSignIn?: () => void;
  /** The public privacy page. */
  privacyHref?: string;
}

interface Resolved { t: KidsText; cls: (kind?: 'primary' | 'danger' | 'link', extra?: string) => string; onSignIn?: () => void; privacyHref: string }

const Ctx = createContext<Resolved | null>(null);

/** Wrap the game (or the part with kids) once. */
export function KidsProvider({ children, lang = 'de', text, button = '', primary = '', danger = '', link, onSignIn, privacyHref = '/datenschutz' }: KidsSettings & { children: ReactNode }) {
  const value = useMemo<Resolved>(() => ({
    t: kidsText(lang, text),
    cls: (kind, extra) => [kind === 'link' ? (link ?? button) : button, kind === 'primary' ? primary : kind === 'danger' ? danger : '', extra ?? '']
      .filter(Boolean).join(' '),
    onSignIn,
    privacyHref,
  }), [lang, text, button, primary, danger, link, onSignIn, privacyHref]);
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

function useKids(): Resolved {
  const c = useContext(Ctx);
  if (!c) throw new Error('kids: wrap the app in <KidsProvider>');
  return c;
}

/** The module's copy, for the game's own places (e.g. a menu item "Datenschutz"). */
export function useKidsText(): KidsText { return useKids().t; }

/** A kid as the tiles show it. */
export interface TileKid {
  id: string;
  name: string;
  /** The photo URL (directory through the game, or a guest's data: URL). */
  photo?: string;
  /** Shown without a photo (the game's animal); default: the first letter of the name. */
  fallback?: ReactNode;
  /** A small line under the name (stars, level). */
  detail?: ReactNode;
  /** false: never edited from the tiles (e.g. the signed-in adult shown as a player). */
  editable?: boolean;
  /** The game's own class and style for this tile (a colour per kid, a ring). */
  className?: string;
  style?: CSSProperties;
}

/** A kid's face: the photo, else the game's fallback, else the initial. */
export function KidFace({ name, photo, fallback, className }: { name: string; photo?: string; fallback?: ReactNode; className?: string }) {
  return (
    <span className={['kids-face', className].filter(Boolean).join(' ')} aria-hidden="true">
      {photo ? <img src={photo} alt="" draggable={false} /> : fallback ?? <span className="kids-initial">{name.trim().slice(0, 1).toUpperCase() || '?'}</span>}
    </span>
  );
}

const Pencil = () => (
  <svg className="kids-pencil" viewBox="0 0 24 24" width="18" height="18" aria-hidden="true">
    <path d="M4 20h4L19 9l-4-4L4 16v4zM14 6l4 4" fill="none" stroke="currentColor" strokeWidth="2" strokeLinejoin="round" />
  </svg>
);

export interface KidTilesProps {
  kids: readonly TileKid[];
  /** The kid playing on this device. */
  active?: string;
  onPick: (id: string) => void;
  /** Nobody signed in: everything stays on this device; the guest note shows under the tiles. */
  guest: boolean;
  /** A guest, a game without directory, or a family admin: "+ Kind" last. */
  canAdd: boolean;
  onAdd: () => void;
  /** Offers "Bearbeiten": while on, a tap on a tile edits that kid. */
  onEdit?: (id: string) => void;
  /** Signed in: guest kids still on this device, with the banner to take them over. */
  waiting?: number;
  onAdopt?: () => void;
  /** Accessible name of the group (the game's question, "Wer baut?"). */
  label?: string;
  className?: string;
}

/** The one place for kids: the takeover banner, a tile per kid, "+ Kind", the note line. */
export function KidTiles({ kids, active, onPick, guest, canAdd, onAdd, onEdit, waiting = 0, onAdopt, label, className }: KidTilesProps) {
  const { t, cls, onSignIn, privacyHref } = useKids();
  const [editing, setEditing] = useState(false);
  const editable = !!onEdit && kids.some((k) => k.editable !== false);
  useEffect(() => { if (!editable) setEditing(false); }, [editable]);
  const editSwitch = editable && (
    <button type="button" className={cls('link', 'kids-edit-switch')} aria-pressed={editing} onClick={() => setEditing((e) => !e)}>
      {editing ? t.done : t.edit}
    </button>
  );
  return (
    <div className={['kids-tiles', editing ? 'kids-editing' : '', className].filter(Boolean).join(' ')}>
      {!guest && waiting > 0 && onAdopt && <AdoptBanner count={waiting} onAdopt={onAdopt} />}
      <div className="kids-grid" role="group" aria-label={label}>
        {kids.map((k) => {
          const edit = editing && k.editable !== false;
          return (
            <button
              key={k.id}
              type="button"
              className={['kids-tile', k.className].filter(Boolean).join(' ')}
              style={k.style}
              aria-pressed={editing ? undefined : k.id === active}
              aria-label={edit ? fill(t.editKid, { name: k.name }) : undefined}
              disabled={editing && !edit}
              onClick={() => (edit && onEdit ? onEdit(k.id) : onPick(k.id))}
            >
              <KidFace name={k.name} photo={k.photo} fallback={k.fallback} />
              <span className="kids-name">{k.name}</span>
              {k.detail && !editing && <span className="kids-detail">{k.detail}</span>}
              {edit && <span className="kids-edit-mark"><Pencil /></span>}
            </button>
          );
        })}
        {canAdd && !editing && (
          <button type="button" className="kids-tile kids-add" onClick={onAdd} aria-label={t.addLabel}>
            <span className="kids-face kids-add-face" aria-hidden="true">+</span>
            <span className="kids-name">{t.add}</span>
          </button>
        )}
      </div>
      {guest ? (
        <p className="kids-note">
          <span>{t.guestNote}</span>
          {onSignIn && <button type="button" className={cls('primary')} onClick={onSignIn}>{t.signIn}</button>}
          <a href={privacyHref}>{t.privacy}</a>
          {editSwitch}
        </p>
      ) : (editSwitch || !canAdd) && (
        <p className="kids-note">
          {!canAdd && <span>{t.adminOnly}</span>}
          {editSwitch}
        </p>
      )}
    </div>
  );
}

/** "{n} Kinder von diesem Gerät warten noch" with "Übernehmen". */
export function AdoptBanner({ count, onAdopt, className }: { count: number; onAdopt: () => void; className?: string }) {
  const { t, cls } = useKids();
  return (
    <div className={['kids-banner', className].filter(Boolean).join(' ')} role="status">
      <span>{count === 1 ? t.waitingOne : fill(t.waiting, { n: count })}</span>
      <button type="button" className={cls('primary')} onClick={onAdopt}>{t.adopt}</button>
    </div>
  );
}

export interface KidFormProps {
  /** The kid to edit; none to add one. */
  kid?: { id: string; name: string; photo?: string };
  /** The preview without a photo (the game's animal for the chosen one). */
  fallback?: ReactNode;
  /** Signed in without rights over the profile: the name stays as directory has it. */
  nameLocked?: boolean;
  /** Saves the name and the game's own fields (the game holds them); returns the kid's id. A
   *  second call after a failed photo passes the id, so the kid is not added twice. */
  onSave: (name: string, id?: string) => Promise<string>;
  /** Stores the photo (a 512 px JPEG); without it the form has no photo field. */
  onPhoto?: (id: string, jpeg: Blob) => Promise<void>;
  onDone: (id: string) => void;
  onCancel: () => void;
  /** Offers "Kind entfernen" (two taps). */
  onDelete?: (id: string) => Promise<void>;
  /** The game's own fields, after name and photo. */
  children?: ReactNode;
  className?: string;
}

/** "Kind anlegen" / "… bearbeiten": Name, Foto (freiwillig), then the game's fields. */
export function KidForm({ kid, fallback, nameLocked, onSave, onPhoto, onDone, onCancel, onDelete, children, className }: KidFormProps) {
  const { t, cls } = useKids();
  const [name, setName] = useState(kid?.name ?? '');
  const [photo, setPhoto] = useState<Blob | null>(null);
  const [preview, setPreview] = useState('');
  const [savedId, setSavedId] = useState(kid?.id);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [confirm, setConfirm] = useState(false);
  const file = useRef<HTMLInputElement>(null);
  useEffect(() => () => { if (preview) URL.revokeObjectURL(preview); }, [preview]);

  const pick = async (f: File | undefined) => {
    if (!f) return;
    setError('');
    try {
      const small = await shrinkPhoto(f);
      setPhoto(small);
      setPreview(URL.createObjectURL(small));
    } catch {
      setError(t.photoUnreadable);
    }
  };
  const clear = () => { setPhoto(null); setPreview(''); };
  const message = (e: unknown) => (e instanceof Error && e.message ? e.message : t.failed);

  const save = async () => {
    setBusy(true);
    setError('');
    let id = savedId;
    try {
      id = await onSave(name.trim(), savedId);
      setSavedId(id);
    } catch (e) {
      setError(message(e));
      setBusy(false);
      return;
    }
    if (photo && onPhoto) {
      try {
        await onPhoto(id, photo);
      } catch (e) {
        // The kid is saved; only the photo is missing. "Speichern" tries the photo again.
        setError(fill(t.savedNoPhoto, { name: name.trim(), error: message(e) }));
        setBusy(false);
        return;
      }
    }
    setBusy(false);
    onDone(id);
  };
  const remove = async () => {
    if (!savedId || !onDelete) return;
    setBusy(true);
    setError('');
    try {
      await onDelete(savedId);
    } catch (e) {
      setError(message(e));
      setBusy(false);
    }
  };

  const shown = preview || kid?.photo || '';
  return (
    <form className={['kids-form', className].filter(Boolean).join(' ')} onSubmit={(e) => { e.preventDefault(); void save(); }}>
      <label className="kids-field">
        <span className="kids-label">{t.name}</span>
        <input value={name} onChange={(e) => setName(e.target.value)} maxLength={30} autoComplete="off" enterKeyHint="done"
          placeholder={t.namePlaceholder} readOnly={nameLocked} aria-describedby={nameLocked ? 'kids-name-locked' : undefined} />
        {nameLocked && <small id="kids-name-locked" className="kids-hint">{t.nameLocked}</small>}
      </label>
      {onPhoto && (
        <div className="kids-field">
          <span className="kids-label">{t.photo} <small className="kids-hint">{t.optional}</small></span>
          <div className="kids-photo-row">
            <KidFace name={name || '?'} photo={shown || undefined} fallback={fallback} className="kids-preview" />
            <button type="button" className={cls()} onClick={() => file.current?.click()} disabled={busy}>
              {shown ? t.otherPhoto : t.pickPhoto}
            </button>
            {photo && <button type="button" className={cls('link')} onClick={clear} disabled={busy}>{t.noPhoto}</button>}
            <input ref={file} type="file" accept="image/*" hidden onChange={(e) => { void pick(e.target.files?.[0]); e.target.value = ''; }} />
          </div>
        </div>
      )}
      {children}
      {error && <p className="kids-error" role="alert">{error}</p>}
      <div className="kids-actions">
        <button type="submit" className={cls('primary')} disabled={busy || !name.trim()} aria-busy={busy}>{busy ? t.saving : t.save}</button>
        <button type="button" className={cls()} onClick={onCancel} disabled={busy}>{t.cancel}</button>
      </div>
      {onDelete && savedId && (
        <div className="kids-actions kids-danger-zone">
          {confirm
            ? <button type="button" className={cls('danger')} onClick={() => void remove()} disabled={busy}>{fill(t.reallyRemove, { name: kid?.name ?? name })}</button>
            : <button type="button" className={cls('link')} onClick={() => setConfirm(true)} disabled={busy}>{t.remove}</button>}
        </div>
      )}
    </form>
  );
}

/** A guest kid still on the device after a sign-in. */
export interface GuestKidRow {
  id: string;
  name: string;
  photo?: string;
  fallback?: ReactNode;
  /** What it made ("3 Burgen · 5 Sterne"). */
  detail?: ReactNode;
}

export interface AdoptProps {
  guests: readonly GuestKidRow[];
  /** The signed-in kids that can take a guest kid over. */
  kids: readonly { id: string; name: string }[];
  /** "als neues Kind": a family admin, or a game without directory. */
  canAdd: boolean;
  /** Moves one guest kid to kid `to`, or to a new kid when `to` is null. */
  onAdopt: (guestId: string, to: string | null) => Promise<void>;
  /** Throws away what guests made on this device. */
  onDrop: () => void | Promise<void>;
  /** Asked once before moving: the names of kids whose own progress a move would replace. */
  checkReplace?: (moves: { guest: string; kid: string }[]) => Promise<string[]>;
  onClose: () => void;
  /** After the takeover or the drop. */
  onDone: () => void;
  /** After a takeover that failed part way (the error shows, the sheet stays): refresh. */
  onChanged?: () => void;
  className?: string;
}

/** After a sign-in: each guest kid goes "zu {Kind}", "als neues Kind" or "noch nicht". */
export function Adopt({ guests, kids, canAdd, onAdopt, onDrop, checkReplace, onClose, onDone, onChanged, className }: AdoptProps) {
  const { t, cls } = useKids();
  const [to, setTo] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [dropping, setDropping] = useState(false);
  const [replacing, setReplacing] = useState<string[] | null>(null);
  const target = (g: GuestKidRow) => to[g.id] ?? suggestTarget(g.name, kids, canAdd);

  const run = async () => {
    setBusy(true);
    setError('');
    try {
      if (checkReplace && !replacing) {
        const moves = guests.flatMap((g) => {
          const k = target(g);
          return k !== AS_NEW && k !== NOT_YET ? [{ guest: g.id, kid: k }] : [];
        });
        const names = moves.length ? await checkReplace(moves) : [];
        if (names.length) {
          setReplacing(names);
          setBusy(false);
          return;
        }
      }
      for (const g of guests) {
        const k = target(g);
        if (k !== NOT_YET) await onAdopt(g.id, k === AS_NEW ? null : k);
      }
      setBusy(false);
      onDone();
    } catch (e) {
      setError(e instanceof Error && e.message ? e.message : t.failed);
      setBusy(false);
      onChanged?.();
    }
  };
  const drop = async () => {
    setBusy(true);
    try { await onDrop(); } finally { setBusy(false); }
    onDone();
  };

  return (
    <div className={['kids-adopt', className].filter(Boolean).join(' ')}>
      <p className="kids-lead">{t.adoptLead}</p>
      <ul className="kids-adopt-list">
        {guests.map((g) => (
          <li key={g.id} className="kids-adopt-row">
            <KidFace name={g.name} photo={g.photo} fallback={g.fallback} />
            <span className="kids-adopt-who">
              <b>{g.name}</b>
              {g.detail && <span className="kids-detail">{g.detail}</span>}
            </span>
            <select aria-label={fill(t.adoptTo, { name: g.name })} value={target(g)} disabled={busy}
              onChange={(e) => { setReplacing(null); setTo((m) => ({ ...m, [g.id]: e.target.value })); }}>
              {kids.map((k) => <option key={k.id} value={k.id}>{fill(t.toKid, { name: k.name })}</option>)}
              {canAdd && <option value={AS_NEW}>{t.asNew}</option>}
              <option value={NOT_YET}>{t.notYet}</option>
            </select>
          </li>
        ))}
      </ul>
      {replacing && <p className="kids-error" role="alert">{fill(t.replace, { names: replacing.join(', ') })}</p>}
      {error && <p className="kids-error" role="alert">{error}</p>}
      <div className="kids-actions">
        <button type="button" className={cls(replacing ? 'danger' : 'primary')} onClick={() => void run()} disabled={busy} aria-busy={busy}>
          {replacing ? t.replaceOk : t.adopt}
        </button>
        <button type="button" className={cls()} onClick={onClose} disabled={busy}>{t.later}</button>
      </div>
      <div className="kids-actions kids-danger-zone">
        {dropping
          ? <button type="button" className={cls('danger')} onClick={() => void drop()} disabled={busy}>{t.reallyDropAll}</button>
          : <button type="button" className={cls('link')} onClick={() => setDropping(true)} disabled={busy}>{t.dropAll}</button>}
      </div>
    </div>
  );
}

export interface AccountActionsProps {
  guest: boolean;
  /** Signed themselves up: may delete what the game keeps of their family. */
  selfRegistered: boolean;
  onSignOut: () => void;
  /** Deletes the family's data in the game; answers whom to ask for the rest. */
  onDelete?: () => Promise<{ contact?: string }>;
  /** What "Konto löschen" deletes, in the game's words. */
  deleteText: string;
  /** Known before the deletion answers (the game's privacy contact). */
  contact?: string;
  className?: string;
}

/** The end of the parents' place, the same order in every game: Anmelden or Abmelden, Konto
 *  löschen (self-registered only), Datenschutz. */
export function AccountActions({ guest, selfRegistered, onSignOut, onDelete, deleteText, contact = '', className }: AccountActionsProps) {
  const { t, cls, onSignIn, privacyHref } = useKids();
  const [confirm, setConfirm] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [done, setDone] = useState<string | null>(null);
  const names = ['kids-account', className].filter(Boolean).join(' ');
  if (guest) {
    return (
      <div className={names}>
        <p>{t.guestNote}</p>
        <div className="kids-actions">
          {onSignIn && <button type="button" className={cls('primary')} onClick={onSignIn}>{t.signIn}</button>}
          <a href={privacyHref}>{t.privacy}</a>
        </div>
      </div>
    );
  }
  const remove = async () => {
    if (!onDelete) return;
    setBusy(true);
    setError('');
    try {
      const r = await onDelete();
      setDone(r.contact || contact);
    } catch (e) {
      setError(e instanceof Error && e.message ? e.message : t.failed);
    }
    setBusy(false);
  };
  if (done !== null) {
    const href = contactHref(done);
    const [before = '', after = ''] = t.contact.split('{contact}');
    return (
      <div className={names} role="status">
        <p>{t.deleted}</p>
        <p>{done ? <>{before}{href ? <a href={href}>{done}</a> : done}{after}</> : t.contactNone}</p>
        <div className="kids-actions"><button type="button" className={cls('primary')} onClick={onSignOut}>{t.signOut}</button></div>
      </div>
    );
  }
  return (
    <div className={names}>
      <div className="kids-actions">
        <button type="button" className={cls()} onClick={onSignOut}>{t.signOut}</button>
        {selfRegistered && onDelete && (
          <button type="button" className={cls('danger')} onClick={() => setConfirm(true)} aria-expanded={confirm}>{t.deleteAccount}</button>
        )}
        <a href={privacyHref}>{t.privacy}</a>
      </div>
      {confirm && (
        <div className="kids-confirm" role="group" aria-label={t.deleteAccount}>
          <p>{deleteText}</p>
          {error && <p className="kids-error" role="alert">{error}</p>}
          <div className="kids-actions">
            <button type="button" className={cls('danger')} onClick={() => void remove()} disabled={busy} aria-busy={busy}>{t.deleteAll}</button>
            <button type="button" className={cls()} onClick={() => setConfirm(false)} disabled={busy}>{t.cancel}</button>
          </div>
        </div>
      )}
    </div>
  );
}
