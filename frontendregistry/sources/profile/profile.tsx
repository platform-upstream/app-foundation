// The signed-in person, their settings and the language (app-foundation registry module `profile`).
// Registry-owned: do not edit in an application. The backend is the Go package `profile` mounted at
// /api/me. Wrap the app in <ProfileProvider messages={…}>; use useT() for every visible string.
import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';

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

interface Ctx {
  profile: Profile | undefined;
  error: string;
  lang: Language;
  t: (key: string, vars?: Record<string, string | number>) => string;
  saveSettings: (s: Settings) => Promise<void>;
  /** Number and date formatting for the language (de-CH / en-GB). */
  locale: string;
}

const ProfileContext = createContext<Ctx | null>(null);

/** Loads /api/me once and provides the profile, the language and t(). The language is the person's
 *  setting, else the browser's; it is cached locally so the first paint is already right. */
export function ProfileProvider({ messages, children, base = '/api/me' }: { messages: Messages; children: ReactNode; base?: string }) {
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

  const value = useMemo<Ctx>(() => ({
    profile, error, lang, saveSettings, locale: lang === 'de' ? 'de-CH' : 'en-GB',
    t: (key, vars) => translate(messages, lang, key, vars),
  }), [profile, error, lang, saveSettings, messages]);
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
export function useProfile() { const c = useCtx(); return { profile: c.profile, error: c.error, saveSettings: c.saveSettings }; }

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

/** Bottom left of the sidebar (desktop) and the top bar (phones, compact): photo, name, role. A
 *  click opens the settings. */
export function ProfileCard({ compact = false, onOpen }: { compact?: boolean; onOpen: () => void }) {
  const { profile } = useProfile();
  const t = useT();
  if (!profile) return compact ? null : <div className="pf-card pf-loading" aria-hidden="true" />;
  const sub = profile.actor ? t('profile.actingFor', { name: profile.name }) : t(`profile.role.${profile.role}`);
  const shown = profile.actor ? { name: profile.actor.name, photo: '' } : profile;
  return (
    <button type="button" className={`pf-card${compact ? ' pf-compact' : ''}`} onClick={onOpen} aria-label={`${t('profile.settings')}: ${shown.name}`}>
      <Avatar name={shown.name} photo={shown.photo} size={compact ? 32 : 36} />
      {!compact && (
        <span className="pf-text">
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
