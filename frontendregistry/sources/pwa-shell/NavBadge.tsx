// A count on a nav item (app-foundation registry module `pwa-shell`). Registry-owned: do not edit
// in an application. Its geometry is in shell.css (.nav-badge); colours, radius and what is
// counted are the application's.

/** The text of a count: 1…99, then "99+". */
export function badgeLabel(count: number): string {
  return count > 99 ? '99+' : String(Math.max(0, Math.floor(count)));
}

export interface NavBadgeProps {
  /** Nothing is shown at 0 (or less). */
  count: number;
  /** The accessible text, the app's own words: "3 open incidents". */
  label: string;
  /** A small dot without the number (the label still says it). */
  dot?: boolean;
  /** Set as data-tone for the app's styling ("danger"). */
  tone?: string;
  className?: string;
}

/**
 * Place it next to the icon inside an element with class `nav-badge-anchor`:
 *
 *   <a href="/inbox"><span className="nav-badge-anchor"><InboxIcon /><NavBadge count={n} label={t('inbox.count', { n })} /></span>Inbox</a>
 */
export function NavBadge({ count, label, dot = false, tone, className }: NavBadgeProps) {
  if (!(count > 0)) return null;
  return (
    <span
      className={className ? `nav-badge ${className}` : 'nav-badge'}
      data-dot={dot ? '' : undefined}
      data-tone={tone}
      role="img"
      aria-label={label}
    >
      {dot ? null : badgeLabel(count)}
    </span>
  );
}
