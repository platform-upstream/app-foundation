// The signed-in person, their settings and the language (app-foundation registry module `profile`).
// Registry-owned: do not edit in an application. The backend is the Go package `profile` mounted at
// /api/me. Wrap the app in <ProfileProvider messages={…}>; use useT() for every visible string.
import { createContext, useCallback, useContext, useEffect, useLayoutEffect, useMemo, useRef, useState, type CSSProperties, type ReactNode } from 'react';
import { createPortal } from 'react-dom';

export type Language = 'de' | 'en';

export interface Space { kind: 'person' | 'family'; id: string }

export interface Profile {
  id: string;
  name: string;
  group: string;
  email: string;
  role: string;
  kid: boolean;
  /** App-relative photo URL, '' without one (show initials). */
  photo: string;
  actor?: { id: string; name: string; role: string };
  spaces: Space[];
  settings: Settings;
}

export interface Settings {
  /** '' follows the browser. */
  language: '' | Language;
  /** The app's own settings. */
  extra: Record<string, unknown>;
}

/** An app's strings: one dictionary per language, same keys. English is the fallback. */
export type Messages = Record<Language, Record<string, string>>;

/** Strings of this module; the app's own messages may override them. */
const OWN: Messages = {
  en: {
    'profile.settings': 'Settings',
    'profile.language': 'Language',
    'profile.language.browser': 'Browser default',
    'profile.close': 'Close',
    'profile.save': 'Save',
    'profile.saving': 'Saving…',
    'profile.saved': 'Saved',
    'profile.actingFor': 'acting for {name}',
    'profile.signedIn': 'Signed in',
    'profile.role.owner': 'Owner',
    'profile.role.member': 'Member',
    'profile.role.viewer': 'Viewer',
    'profile.spaces': 'Your data',
    'profile.space.own': 'Your own',
    'profile.space.family': 'Shared with family {id}',
    'profile.menu': 'Menu',
    'profile.theme': 'Appearance',
    'profile.theme.auto': 'Auto',
    'profile.theme.light': 'Light',
    'profile.theme.dark': 'Dark',
    'profile.apps': 'Other apps',
    'profile.apps.none': 'No other apps.',
    'profile.back': 'Back',
    'profile.saveFailed': 'Not saved: {error}',
    'profile.lang.auto': 'Auto',
  },
  de: {
    'profile.settings': 'Einstellungen',
    'profile.language': 'Sprache',
    'profile.language.browser': 'Wie im Browser',
    'profile.close': 'Schliessen',
    'profile.save': 'Speichern',
    'profile.saving': 'Speichert…',
    'profile.saved': 'Gespeichert',
    'profile.actingFor': 'für {name}',
    'profile.signedIn': 'Angemeldet',
    'profile.role.owner': 'Besitzer',
    'profile.role.member': 'Mitglied',
    'profile.role.viewer': 'Leser',
    'profile.spaces': 'Deine Daten',
    'profile.space.own': 'Deine eigenen',
    'profile.space.family': 'Geteilt mit Familie {id}',
    'profile.menu': 'Menü',
    'profile.theme': 'Darstellung',
    'profile.theme.auto': 'Auto',
    'profile.theme.light': 'Hell',
    'profile.theme.dark': 'Dunkel',
    'profile.apps': 'Andere Apps',
    'profile.apps.none': 'Keine anderen Apps.',
    'profile.back': 'Zurück',
    'profile.saveFailed': 'Nicht gespeichert: {error}',
    'profile.lang.auto': 'Auto',
  },
};

/** The browser's language if supported, else English. */
export function browserLanguage(langs: readonly string[] = typeof navigator === 'undefined' ? [] : navigator.languages ?? [navigator.language]): Language {
  for (const l of langs) {
    const base = l.toLowerCase().split('-')[0];
    if (base === 'de' || base === 'en') return base;
  }
  return 'en';
}

/** Looks a key up in the language, then English, then shows the key; {name} placeholders are filled. */
export function translate(messages: Messages, lang: Language, key: string, vars?: Record<string, string | number>): string {
  let s = messages[lang]?.[key] ?? messages.en?.[key] ?? OWN[lang][key] ?? OWN.en[key] ?? key;
  if (vars) for (const [k, v] of Object.entries(vars)) s = s.split(`{${k}}`).join(String(v));
  return s;
}

const LOCAL_KEY = 'profile.language';

const THEME_KEY = 'profile.theme';

/** The person's appearance choice (settings.extra.theme); 'auto' follows the system. */
export type Theme = 'auto' | 'light' | 'dark';

function asTheme(v: unknown): Theme { return v === 'light' || v === 'dark' ? v : 'auto'; }

function systemDark(): boolean {
  return typeof matchMedia !== 'undefined' && matchMedia('(prefers-color-scheme: dark)').matches;
}

interface Ctx {
  profile: Profile | undefined;
  error: string;
  lang: Language;
  t: (key: string, vars?: Record<string, string | number>) => string;
  saveSettings: (s: Settings) => Promise<void>;
  /** Saves at once, shown before the server answers (reverted if it fails). */
  updateSettings: (patch: { language?: '' | Language; extra?: Record<string, unknown> }) => Promise<void>;
  /** Number and date formatting for the language (de-CH / en-GB). */
  locale: string;
  /** Where /api/me is mounted. */
  base: string;
  /** The app offers light and dark (ProfileProvider themes). */
  themes: boolean;
  theme: Theme;
}

const ProfileContext = createContext<Ctx | null>(null);

/** Loads /api/me once and provides the profile, the language and t(). The language is the person's
 *  setting, else the browser's; it is cached locally so the first paint is already right.
 *  themes: the app has a dark look; <html data-theme="light|dark"> then follows the person's choice
 *  (settings.extra.theme, else the system), and the menu offers it. */
export function ProfileProvider({ messages, children, base = '/api/me', themes = false }: { messages: Messages; children: ReactNode; base?: string; themes?: boolean }) {
  const [profile, setProfile] = useState<Profile>();
  const [error, setError] = useState('');
  const [stored, setStored] = useState<'' | Language>(() => {
    const v = typeof localStorage === 'undefined' ? '' : localStorage.getItem(LOCAL_KEY);
    return v === 'de' || v === 'en' ? v : '';
  });

  useEffect(() => {
    let live = true;
    fetch(base, { credentials: 'same-origin', redirect: 'manual', headers: { Accept: 'application/json' } })
      .then(async (r) => {
        if (r.type === 'opaqueredirect') { location.reload(); return; }
        if (!r.ok) throw new Error(`profile ${r.status}`);
        const p = (await r.json()) as Profile;
        if (!live) return;
        p.settings ??= { language: '', extra: {} };
        p.settings.extra ??= {};
        setProfile(p);
        setStored(p.settings.language);
        localStorage.setItem(LOCAL_KEY, p.settings.language);
      })
      .catch((e: unknown) => live && setError(e instanceof Error ? e.message : String(e)));
    return () => { live = false; };
  }, [base]);

  const lang = stored || browserLanguage();
  useEffect(() => { document.documentElement.lang = lang; }, [lang]);

  const [theme, setTheme] = useState<Theme>(() => asTheme(typeof localStorage === 'undefined' ? '' : localStorage.getItem(THEME_KEY)));
  useEffect(() => { if (profile) setTheme(asTheme(profile.settings.extra.theme)); }, [profile]);
  useEffect(() => {
    if (!themes) return;
    localStorage.setItem(THEME_KEY, theme);
    const root = document.documentElement;
    const apply = () => {
      const dark = theme === 'dark' || (theme === 'auto' && systemDark());
      root.dataset.theme = dark ? 'dark' : 'light';
      root.style.colorScheme = dark ? 'dark' : 'light';
    };
    apply();
    if (theme !== 'auto' || typeof matchMedia === 'undefined') return;
    const mq = matchMedia('(prefers-color-scheme: dark)');
    mq.addEventListener('change', apply);
    return () => mq.removeEventListener('change', apply);
  }, [themes, theme]);

  const saveSettings = useCallback(async (s: Settings) => {
    const r = await fetch(`${base}/settings`, {
      method: 'PUT', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', Accept: 'application/json' }, body: JSON.stringify(s),
    });
    if (!r.ok) {
      const body = (await r.json().catch(() => ({}))) as { error?: string };
      throw new Error(body.error || `settings ${r.status}`);
    }
    const saved = (await r.json()) as Settings;
    setStored(saved.language);
    localStorage.setItem(LOCAL_KEY, saved.language);
    setProfile((p) => (p ? { ...p, settings: saved } : p));
  }, [base]);

  const updateSettings = useCallback(async (patch: { language?: '' | Language; extra?: Record<string, unknown> }) => {
    const before = profile;
    if (!before) return;
    const next: Settings = {
      language: patch.language ?? before.settings.language,
      extra: { ...before.settings.extra, ...patch.extra },
    };
    setProfile({ ...before, settings: next });
    if (patch.language !== undefined) setStored(patch.language);
    try {
      await saveSettings(next);
    } catch (e) {
      setProfile(before);
      setStored(before.settings.language);
      throw e;
    }
  }, [profile, saveSettings]);

  const value = useMemo<Ctx>(() => ({
    profile, error, lang, saveSettings, updateSettings, base, themes, theme, locale: lang === 'de' ? 'de-CH' : 'en-GB',
    t: (key, vars) => translate(messages, lang, key, vars),
  }), [profile, error, lang, saveSettings, updateSettings, base, themes, theme, messages]);
  return <ProfileContext.Provider value={value}>{children}</ProfileContext.Provider>;
}

function useCtx(): Ctx {
  const c = useContext(ProfileContext);
  if (!c) throw new Error('profile: wrap the app in <ProfileProvider>');
  return c;
}

/** t() for the current language. */
export function useT() { return useCtx().t; }
export function useLanguage() { const c = useCtx(); return { lang: c.lang, locale: c.locale }; }
export function useProfile() { const c = useCtx(); return { profile: c.profile, error: c.error, saveSettings: c.saveSettings, updateSettings: c.updateSettings }; }
/** The app's own setting key (settings.extra), with a fallback until the profile has loaded. */
export function useSetting<T>(key: string, fallback: T): T {
  const v = useCtx().profile?.settings.extra[key];
  return v === undefined ? fallback : (v as T);
}
export function useTheme() { const c = useCtx(); return { theme: c.theme, enabled: c.themes }; }

export function initials(name: string): string {
  const w = name.trim().split(/\s+/).filter(Boolean);
  return ((w.length > 1 ? w[0]![0]! + w[w.length - 1]![0]! : (w[0] ?? '?').slice(0, 2))).toUpperCase();
}

export function Avatar({ name, photo, size = 36 }: { name: string; photo: string; size?: number }) {
  const [broken, setBroken] = useState(false);
  if (photo && !broken) {
    return <img className="pf-avatar" src={photo} alt="" width={size} height={size} style={{ width: size, height: size }} onError={() => setBroken(true)} />;
  }
  return <span className="pf-avatar pf-initials" style={{ width: size, height: size, fontSize: Math.round(size * 0.38) }} aria-hidden="true">{initials(name)}</span>;
}

/**
 * The accessible name of the profile button. The full card names itself through its visible text
 * (name and role, after a visually hidden `what`), so it needs no label. The compact one shows
 * only the photo or the initials: its label is `what: name`, plus the initials when they are what
 * is on screen, since an accessible name must contain the visible text (WCAG 2.5.3, axe
 * label-content-name-mismatch).
 */
export function profileButtonLabel(what: string, person: { name: string; photo: string }, compact: boolean): string | undefined {
  if (!compact) return undefined;
  return `${what}: ${person.name}${person.photo ? '' : `, ${initials(person.name)}`}`;
}

/** Bottom left of the sidebar (desktop) and the top bar (phones, compact): photo, name, role. A
 *  click opens the settings. */
export function ProfileCard({ compact = false, onOpen }: { compact?: boolean; onOpen: () => void }) {
  const { profile } = useProfile();
  const t = useT();
  if (!profile) return compact ? null : <div className="pf-card pf-loading" aria-hidden="true" />;
  const sub = profile.actor ? t('profile.actingFor', { name: profile.name }) : t(`profile.role.${profile.role}`);
  const shown = profile.actor ? { name: profile.actor.name, photo: '' } : profile;
  return (
    <button type="button" className={`pf-card${compact ? ' pf-compact' : ''}`} onClick={onOpen} aria-label={profileButtonLabel(t('profile.settings'), shown, compact)}>
      <Avatar name={shown.name} photo={shown.photo} size={compact ? 32 : 36} />
      {!compact && (
        <span className="pf-text">
          <span className="pf-sr">{t('profile.settings')}: </span>
          <b>{shown.name}</b>
          <span>{sub}</span>
        </span>
      )}
      {!compact && <svg className="pf-gear" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><circle cx="12" cy="12" r="3" /><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z" /></svg>}
    </button>
  );
}

/** The settings dialog: the profile from directory, the language, and the app's own fields
 *  (children get the draft of extra and a setter). */
export function SettingsSheet({ open, onClose, children }: {
  open: boolean;
  onClose: () => void;
  children?: (extra: Record<string, unknown>, set: (key: string, value: unknown) => void) => ReactNode;
}) {
  const { profile, saveSettings } = useProfile();
  const t = useT();
  const [draft, setDraft] = useState<Settings>({ language: '', extra: {} });
  const [state, setState] = useState<'' | 'saving' | 'saved' | string>('');
  useEffect(() => { if (open && profile) { setDraft(structuredClone(profile.settings)); setState(''); } }, [open, profile]);
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose(); };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [open, onClose]);
  if (!open || !profile) return null;
  const save = async () => {
    setState('saving');
    try { await saveSettings(draft); setState('saved'); setTimeout(onClose, 400); } catch (e) { setState(e instanceof Error ? e.message : String(e)); }
  };
  const langs: { id: '' | Language; label: string }[] = [
    { id: '', label: t('profile.language.browser') }, { id: 'de', label: 'Deutsch' }, { id: 'en', label: 'English' },
  ];
  return (
    <div className="pf-backdrop" onClick={(e) => { if (e.target === e.currentTarget) onClose(); }}>
      <div className="pf-sheet" role="dialog" aria-modal="true" aria-labelledby="pf-title">
        <div className="pf-head">
          <Avatar name={profile.name} photo={profile.photo} size={56} />
          <div className="pf-who">
            <h2 id="pf-title">{profile.name}</h2>
            <span>{profile.email || t(`profile.role.${profile.role}`)}</span>
          </div>
          <button type="button" className="pf-x" onClick={onClose} aria-label={t('profile.close')}>×</button>
        </div>
        <fieldset className="pf-field">
          <legend>{t('profile.language')}</legend>
          <div className="pf-seg" role="radiogroup" aria-label={t('profile.language')}>
            {langs.map((l) => (
              <button key={l.id || 'browser'} type="button" role="radio" aria-checked={draft.language === l.id}
                className={draft.language === l.id ? 'on' : ''} onClick={() => setDraft({ ...draft, language: l.id })}>{l.label}</button>
            ))}
          </div>
        </fieldset>
        {children?.(draft.extra, (key, value) => setDraft((d) => ({ ...d, extra: { ...d.extra, [key]: value } })))}
        {profile.spaces.length > 1 && (
          <div className="pf-field">
            <span className="pf-label">{t('profile.spaces')}</span>
            <ul className="pf-spaces">
              {profile.spaces.map((s) => <li key={s.kind + s.id}>{s.kind === 'person' ? t('profile.space.own') : t('profile.space.family', { id: s.id })}</li>)}
            </ul>
          </div>
        )}
        <div className="pf-actions">
          {state && state !== 'saving' && state !== 'saved' && <span className="pf-error" role="alert">{state}</span>}
          <button type="button" className="pf-btn" onClick={onClose}>{t('profile.close')}</button>
          <button type="button" className="pf-btn pf-pri" disabled={state === 'saving'} onClick={() => void save()}>
            {state === 'saving' ? t('profile.saving') : state === 'saved' ? t('profile.saved') : t('profile.save')}
          </button>
        </div>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------------------------
// The menu: the person, language and appearance, the app's own entries, and the person's other
// apps. Choices save at once (no Save button); the app declares its entries as data.

export type MenuItem =
  /** A switch stored in settings.extra[key]. */
  | { kind: 'toggle'; key: string; label: string; hint?: string; default?: boolean }
  /** One of a few values, stored in settings.extra[key]. */
  | { kind: 'choice'; key: string; label: string; options: { value: string; label: string }[]; default?: string }
  /** Runs something in the app; the menu closes unless keepOpen. */
  | { kind: 'action'; label: string; hint?: string; icon?: ReactNode; onSelect: () => void; disabled?: boolean; keepOpen?: boolean }
  /** Opens a page (in the app, or elsewhere with external). */
  | { kind: 'link'; label: string; href: string; hint?: string; icon?: ReactNode; external?: boolean }
  /** A fact about the person in this app. */
  | { kind: 'info'; label: string; value: ReactNode; icon?: ReactNode };

export interface MenuSection { title?: string; items: MenuItem[] }

/** One of the person's other apps (GET {base}/apps; anything but 200 hides the list). */
export interface OtherApp { key: string; name: string; url: string; color?: string }

const TONES = ['#C2185B', '#2F7D8C', '#6B8F5E', '#E0A21B', '#3C6FD1', '#8C6A4A', '#D2453A', '#4E7A63', '#B8478A', '#E8622F'];
function tone(key: string): string {
  let h = 0;
  for (const c of key) h = (h * 31 + c.charCodeAt(0)) >>> 0;
  return TONES[h % TONES.length]!;
}

function useOtherApps(open: boolean): OtherApp[] | undefined {
  const { base } = useCtx();
  const [apps, setApps] = useState<OtherApp[]>();
  const asked = useRef(false);
  useEffect(() => {
    if (!open || asked.current) return;
    asked.current = true;
    fetch(`${base}/apps`, { credentials: 'same-origin', redirect: 'manual', headers: { Accept: 'application/json' } })
      .then(async (r) => (r.ok ? ((await r.json()) as OtherApp[]) : []))
      .then((list) => setApps(Array.isArray(list) ? list.filter((a) => /^https:\/\//.test(a.url)) : []))
      .catch(() => setApps([]));
  }, [open, base]);
  return apps;
}

function Glyph({ d, size = 18 }: { d: ReactNode; size?: number }) {
  return <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">{d}</svg>;
}

function Segmented<V extends string>({ label, value, options, onChange }: { label: string; value: V; options: { value: V; label: string }[]; onChange: (v: V) => void }) {
  return (
    <div className={`pf-row${options.length > 3 ? ' pf-row-stack' : ''}`}>
      <span className="pf-row-label">{label}</span>
      <div className="pf-seg" role="radiogroup" aria-label={label}>
        {options.map((o) => (
          <button key={o.value || 'auto'} type="button" role="radio" aria-checked={value === o.value}
            className={value === o.value ? 'on' : ''} onClick={() => onChange(o.value)}>{o.label}</button>
        ))}
      </div>
    </div>
  );
}

type Patch = { language?: '' | Language; extra?: Record<string, unknown> };

/** The profile card that opens the menu. Bottom left of the sidebar it opens upwards; compact (the
 *  photo alone, top bar on phones) it opens downwards. Under 700px it is a bottom sheet. */
export function ProfileMenu({ sections = [], compact = false, apps = true }: { sections?: MenuSection[]; compact?: boolean; apps?: boolean }) {
  const c = useCtx();
  const { profile, updateSettings, themes, theme, t } = c;
  const [open, setOpen] = useState(false);
  const [err, setErr] = useState('');
  const others = useOtherApps(open && apps);
  const wrap = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const [place, setPlace] = useState<CSSProperties>({});

  // The panel lives in <body> (no sidebar overflow or header stacking clips it); on wide screens
  // it is placed beside the card: above it from the sidebar, below it from the top bar.
  useLayoutEffect(() => {
    if (!open) return;
    const at = () => {
      const r = trigger.current?.getBoundingClientRect();
      if (!r || innerWidth < 700) { setPlace({}); return; }
      const width = Math.min(340, innerWidth - 16);
      const left = Math.max(8, Math.min(compact ? r.right - width : r.left, innerWidth - width - 8));
      setPlace(compact ? { left, top: r.bottom + 8, width } : { left, bottom: innerHeight - r.top + 8, width });
    };
    at();
    addEventListener('resize', at);
    return () => removeEventListener('resize', at);
  }, [open, compact]);

  const close = useCallback((focus = true) => {
    setOpen(false);
    if (focus) trigger.current?.focus();
  }, []);
  useEffect(() => {
    if (!open) return;
    setErr('');
    panel.current?.querySelector<HTMLElement>('button, a[href]')?.focus();
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') { e.preventDefault(); close(); } };
    const onDown = (e: PointerEvent) => {
      const n = e.target as Node;
      if (!wrap.current?.contains(n) && !panel.current?.contains(n)) close(false);
    };
    window.addEventListener('keydown', onKey);
    document.addEventListener('pointerdown', onDown);
    return () => { window.removeEventListener('keydown', onKey); document.removeEventListener('pointerdown', onDown); };
  }, [open, close]);

  if (!profile) return compact ? null : <div className="pf-card pf-loading" aria-hidden="true" />;
  const save = (patch: Patch) => {
    setErr('');
    updateSettings(patch).catch((e: unknown) => setErr(t('profile.saveFailed', { error: e instanceof Error ? e.message : String(e) })));
  };
  const shown = profile.actor ? { name: profile.actor.name, photo: '' } : profile;
  const sub = profile.actor ? t('profile.actingFor', { name: profile.name }) : t(`profile.role.${profile.role}`);
  const extra = profile.settings.extra;

  return (
    <div ref={wrap} className={`pf-menu-wrap${compact ? ' pf-compact-wrap' : ''}`}>
      <button ref={trigger} type="button" className={`pf-card${compact ? ' pf-compact' : ''}${open ? ' pf-open' : ''}`}
        aria-haspopup="dialog" aria-expanded={open} aria-label={profileButtonLabel(t('profile.menu'), shown, compact)} onClick={() => setOpen((o) => !o)}>
        <Avatar name={shown.name} photo={shown.photo} size={compact ? 32 : 36} />
        {!compact && <span className="pf-text"><span className="pf-sr">{t('profile.menu')}: </span><b>{shown.name}</b><span>{sub}</span></span>}
        {!compact && <span className="pf-chev" aria-hidden="true"><Glyph d={<path d="M8 10l4-4 4 4M8 14l4 4 4-4" />} size={16} /></span>}
      </button>
      {open && createPortal(
        <>
          <div className="pf-scrim" aria-hidden="true" onClick={() => close(false)} />
          <div ref={panel} className={`pf-menu ${compact ? 'pf-down' : 'pf-up'}`} style={place} role="dialog" aria-label={t('profile.menu')}>
            <div className="pf-grab" aria-hidden="true" />
            <div className="pf-menu-head">
              <Avatar name={shown.name} photo={shown.photo} size={48} />
              <div className="pf-who">
                <b>{shown.name}</b>
                <span>{profile.actor ? sub : [profile.email, t(`profile.role.${profile.role}`)].filter(Boolean).join(' · ')}</span>
              </div>
            </div>
            <div className="pf-group">
              <Segmented label={t('profile.language')} value={profile.settings.language}
                options={[{ value: '' as const, label: t('profile.lang.auto') }, { value: 'de' as const, label: 'DE' }, { value: 'en' as const, label: 'EN' }]}
                onChange={(v) => save({ language: v })} />
              {themes && (
                <Segmented label={t('profile.theme')} value={theme}
                  options={(['auto', 'light', 'dark'] as const).map((v) => ({ value: v, label: t(`profile.theme.${v}`) }))}
                  onChange={(v) => save({ extra: { theme: v } })} />
              )}
            </div>
            {sections.filter((s) => s.items.length).map((s, i) => (
              <div className="pf-group" key={s.title ?? i}>
                {s.title && <div className="pf-group-title">{s.title}</div>}
                {s.items.map((it, j) => <Item key={j} item={it} extra={extra} save={save} close={close} />)}
              </div>
            ))}
            {apps && others && others.length > 0 && (
              <div className="pf-group">
                <div className="pf-group-title">{t('profile.apps')}</div>
                <div className="pf-apps">
                  {others.map((a) => (
                    <a key={a.key} className="pf-app" href={a.url}>
                      <span className="pf-app-ic" style={{ background: a.color || tone(a.key) }} aria-hidden="true">{a.name.trim().charAt(0).toUpperCase()}</span>
                      <span className="pf-app-name">{a.name}</span>
                    </a>
                  ))}
                </div>
              </div>
            )}
            {err && <div className="pf-error" role="alert">{err}</div>}
          </div>
        </>,
        document.body,
      )}
    </div>
  );
}

function Item({ item, extra, save, close }: {
  item: MenuItem; extra: Record<string, unknown>; save: (p: Patch) => void; close: (focus?: boolean) => void;
}) {
  switch (item.kind) {
    case 'toggle': {
      const on = (extra[item.key] ?? item.default ?? false) === true;
      return (
        <button type="button" className="pf-item" role="switch" aria-checked={on} onClick={() => save({ extra: { [item.key]: !on } })}>
          <span className="pf-item-text"><span>{item.label}</span>{item.hint && <small>{item.hint}</small>}</span>
          <span className={`pf-switch${on ? ' on' : ''}`} aria-hidden="true"><i /></span>
        </button>
      );
    }
    case 'choice': {
      const v = String(extra[item.key] ?? item.default ?? item.options[0]?.value ?? '');
      return <Segmented label={item.label} value={v} options={item.options} onChange={(x) => save({ extra: { [item.key]: x } })} />;
    }
    case 'action':
      return (
        <button type="button" className="pf-item" disabled={item.disabled} onClick={() => { item.onSelect(); if (!item.keepOpen) close(false); }}>
          {item.icon && <span className="pf-item-ic">{item.icon}</span>}
          <span className="pf-item-text"><span>{item.label}</span>{item.hint && <small>{item.hint}</small>}</span>
        </button>
      );
    case 'link':
      return (
        <a className="pf-item" href={item.href} {...(item.external ? { target: '_blank', rel: 'noopener noreferrer' } : {})} onClick={() => close(false)}>
          {item.icon && <span className="pf-item-ic">{item.icon}</span>}
          <span className="pf-item-text"><span>{item.label}</span>{item.hint && <small>{item.hint}</small>}</span>
          <span className="pf-item-end"><Glyph d={item.external ? <><path d="M14 5h5v5" /><path d="M19 5l-8 8" /><path d="M18 14v5H5V6h5" /></> : <path d="M9 6l6 6-6 6" />} size={16} /></span>
        </a>
      );
    default:
      return (
        <div className="pf-item pf-info">
          {item.icon && <span className="pf-item-ic">{item.icon}</span>}
          <span className="pf-item-text"><span>{item.label}</span></span>
          <span className="pf-item-val">{item.value}</span>
        </div>
      );
  }
}
