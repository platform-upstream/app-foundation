/**
 * Phone audit (app-foundation registry module `phone-audit`). Registry-owned: do not edit in an
 * application; `frontend-registry check` fails on any change. The application feeds its routes and
 * fixtures through phone-audit-tests.ts (or calls these helpers from its own specs).
 *
 * Checks one loaded page the way a phone sees it (touch, phone viewport, service worker blocked):
 *
 *   page-overflow      the document is wider than the window
 *   wide-element       an element reaches past the window edge outside a horizontal scroller
 *   sideways-pan       a real touch swipe moves the page, the visual viewport or the main scroller
 *                      sideways, zooms, or navigates (Chromium only: CDP touch events)
 *   text-overflow      text runs out of its button, link or other control (ellipsis and line clamp
 *                      are intended and pass; visually hidden text such as .sr-only and absolutely
 *                      positioned children and pseudo-elements, as a NavBadge or .hit-target's
 *                      ::after, are left out)
 *   covered-target     a visible control a tap cannot reach: at two of three points (centre, two
 *                      inset corners) elementFromPoint finds something else on top (an overlay,
 *                      a sticky bar, a neighbour's hit area), at every scroll step it was on screen
 *                      (the document or the scroller is scrolled in up to 8 steps)
 *   page-error,        with phoneAudit (or watchErrors): an uncaught exception, a console error or
 *   console-error,     a failed request while the route was open; `allowErrors` accepts the
 *   request-failed     application's documented noise
 *   number-split       a number is broken across two lines
 *   tap-target         a control is smaller than 44 x 44 (an invisible ::before/::after that grows
 *                      the target, as .hit-target in `touch` or a stretched link's ::after over its
 *                      positioned card, counts; inline links in text pass)
 *   input-font         a field's font is under 16px (iOS zooms in on focus)
 *   small-text         text under 14px
 *   autofocus          a field is focused right after load (on touch the keyboard opens and
 *                      `pwa-shell` drops the nav)
 *   layout-shift       something moves during a refetch window (Layout Instability API, Chromium
 *                      only; needs installLayoutShiftObserver before page.goto)
 *   axe                axe-core violations of wcag2a, wcag2aa, wcag21a, wcag21aa, wcag22aa
 *                      (`@axe-core/playwright` as the application's devDependency)
 *   running-animation  with reducedMotion 'reduce', an animation or transition still runs after
 *                      load (auditReducedMotion)
 *
 * plus the platform's own rules (`platformRules`, for example forbidComputedStyle). What Chromium
 * cannot show (the status bar, lvh/dvh, the keyboard, haptics) stays a check on a real iPhone.
 */
import type { BrowserContextOptions, CDPSession, Page } from '@playwright/test';

/** One phone size the audit runs at. */
export interface PhoneViewport {
  name: string;
  width: number;
  height: number;
  deviceScaleFactor: number;
}

/** The smallest phone still in use (iPhone SE) and the common one (iPhone 12 to 16). */
export const PHONE_VIEWPORTS: readonly PhoneViewport[] = [
  { name: '375x667', width: 375, height: 667, deviceScaleFactor: 2 },
  { name: '390x844', width: 390, height: 844, deviceScaleFactor: 3 },
];

/** Context options for a phone: touch, mobile viewport, no service worker (it would serve stale builds and hide routes). */
export function phoneContextOptions(viewport: PhoneViewport, extra: BrowserContextOptions = {}): BrowserContextOptions {
  return {
    viewport: { width: viewport.width, height: viewport.height },
    deviceScaleFactor: viewport.deviceScaleFactor,
    isMobile: true,
    hasTouch: true,
    serviceWorkers: 'block',
    locale: 'de-CH',
    ...extra,
  };
}

/** One problem on one page. `selector` names the element where there is one. */
export interface Finding {
  check: string;
  detail: string;
  selector?: string;
  text?: string;
}

/** A platform's own rule, run on the loaded page after the built-in checks. */
export interface PlatformRule {
  name: string;
  run: (page: Page) => Promise<Finding[]>;
}

export interface AuditOptions {
  /** Checks to leave out, by name. */
  skip?: string[];
  /** Elements matching this selector (and everything inside them) are not reported, nor count as a cover. */
  ignore?: string;
  /** Extra selector for the application's own controls (`.chip, .seg > *`), judged like buttons. */
  controls?: string;
  /** The page's own scroller, swiped and measured with the window. Default: `.app-shell-main` (pwa-shell). */
  scroller?: string;
  /** Floors: tap target edge (44), field font (16), text (14), all in CSS pixels. */
  minTarget?: number;
  minInputFont?: number;
  minText?: number;
  /** axe-core tags and rules to disable; `false` turns axe off. */
  axe?: { tags?: string[]; disableRules?: string[] } | false;
  /**
   * Starts a refetch for the layout-shift window. Default: a `visibilitychange` and a `focus`
   * event, which TanStack Query treats as a return to the app (it refetches only stale queries:
   * run the audit with staleTime 0, or pass a refetch that invalidates or serves changed data).
   */
  refetch?: (page: Page) => Promise<void>;
  /** How long after the refetch layout shifts count (ms, default 1500). */
  refetchWindowMs?: number;
  /** The largest summed layout-shift score allowed in that window (default 0.001: none). */
  maxLayoutShift?: number;
  platformRules?: PlatformRule[];
  /** Scroll steps of covered-target after the first look (default 8, each 80 % of the scroller). */
  coverSteps?: number;
  /**
   * Page errors the application accepts (phoneAudit's error gate, watchErrors): return true for
   * documented noise, as a request its mocks fail on purpose. Default: every error counts.
   */
  allowErrors?: (error: PageError) => boolean;
}

const WCAG_TAGS = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa'];

/**
 * Records layout shifts from the first paint on (`window.__phoneAuditShifts`). Call before
 * page.goto; a no-op where the browser has no Layout Instability API (WebKit, Firefox).
 */
export async function installLayoutShiftObserver(page: Page): Promise<void> {
  await page.addInitScript(() => {
    const store: { supported: boolean; entries: { value: number; sources: string[] }[] } = { supported: false, entries: [] };
    (window as unknown as { __phoneAuditShifts: typeof store }).__phoneAuditShifts = store;
    if (typeof PerformanceObserver === 'undefined' || !PerformanceObserver.supportedEntryTypes?.includes('layout-shift')) return;
    store.supported = true;
    const name = (node: Node | null) => {
      if (!node || !(node instanceof Element)) return '';
      const classes = typeof node.className === 'string' ? node.className.trim().split(/\s+/).filter(Boolean).slice(0, 2) : [];
      return node.tagName.toLowerCase() + (node.id ? '#' + node.id : '') + classes.map((c) => '.' + c).join('');
    };
    new PerformanceObserver((list) => {
      for (const entry of list.getEntries() as (PerformanceEntry & { value: number; hadRecentInput: boolean; sources?: { node: Node | null }[] })[]) {
        if (entry.hadRecentInput) continue;
        store.entries.push({ value: entry.value, sources: (entry.sources ?? []).map((source) => name(source.node)).filter(Boolean) });
      }
    }).observe({ type: 'layout-shift', buffered: true });
  });
}

/**
 * Waits until the page is quiet: network idle (best effort), web fonts loaded and, unless
 * `animations` is false, the finite animations finished (at most 3s).
 */
export async function settle(page: Page, { animations = true }: { animations?: boolean } = {}): Promise<void> {
  await page.waitForLoadState('networkidle', { timeout: 8000 }).catch(() => {});
  await page.evaluate(async (waitForAnimations) => {
    await document.fonts?.ready;
    if (waitForAnimations) {
      const finite = document.getAnimations().filter((a) => a.effect?.getTiming().iterations !== Infinity);
      await Promise.race([Promise.allSettled(finite.map((a) => a.finished)), new Promise((resolve) => setTimeout(resolve, 3000))]);
    }
    await new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve)));
  }, animations);
}

/** Opens `path`, waits for `ready` (the application's "data is on screen") and settles. */
export async function openRoute(page: Page, path: string, ready?: (page: Page) => Promise<void>, settleOptions?: { animations?: boolean }): Promise<void> {
  await page.goto(path, { waitUntil: 'domcontentloaded' });
  if (ready) await ready(page);
  await settle(page, settleOptions);
}

interface DomOptions {
  ignore: string;
  controls: string;
  scroller: string;
  minTarget: number;
  minInputFont: number;
  minText: number;
  skip: string[];
}

/** The DOM checks, run inside the page. Self-contained: Playwright serializes it. */
function domChecks(options: DomOptions): Finding[] {
  const findings: Finding[] = [];
  const seen = new Set<string>();
  const skip = new Set(options.skip);
  const add = (finding: Finding) => {
    const key = finding.check + '|' + (finding.selector ?? '') + '|' + (finding.text ?? '') + '|' + (finding.selector ? '' : finding.detail);
    if (seen.has(key) || skip.has(finding.check)) return;
    seen.add(key);
    findings.push(finding);
  };
  const ignored = (el: Element) => !!options.ignore && !!el.closest(options.ignore);
  const visible = (el: Element) => {
    const rect = el.getBoundingClientRect();
    if (rect.width < 1 || rect.height < 1) return null;
    const style = getComputedStyle(el);
    if (style.visibility === 'hidden' || style.display === 'none' || el.closest('[inert], [aria-hidden="true"]')) return null;
    return rect;
  };
  const describe = (el: Element): string => {
    const part = (node: Element) => {
      if (node.id) return node.tagName.toLowerCase() + '#' + CSS.escape(node.id);
      const classes = typeof node.className === 'string' ? node.className.trim().split(/\s+/).filter(Boolean).slice(0, 2) : [];
      let out = node.tagName.toLowerCase() + classes.map((c) => '.' + CSS.escape(c)).join('');
      const parent = node.parentElement;
      if (parent) {
        const same = [...parent.children].filter((child) => child.tagName === node.tagName);
        if (same.length > 1) out += `:nth-of-type(${same.indexOf(node) + 1})`;
      }
      return out;
    };
    const parts: string[] = [];
    for (let node: Element | null = el; node && node !== document.body && parts.length < 4; node = node.parentElement) {
      parts.unshift(part(node));
      if (node.id) break;
    }
    return parts.join(' > ');
  };
  // Visually hidden text for screen readers (.sr-only: a 1px absolute box, clip or clip-path) is
  // not on screen, so it neither overflows nor splits nor is small.
  const visuallyHidden = (el: Element) => {
    for (let a: Element | null = el; a && a !== document.body; a = a.parentElement) {
      const style = getComputedStyle(a);
      if (style.position !== 'absolute' && style.position !== 'fixed') continue;
      const rect = a.getBoundingClientRect();
      if ((rect.width <= 1 && rect.height <= 1) || /^rect\(0(px)?,? 0(px)?,? 0(px)?,? 0(px)?\)$/.test(style.clip) || /^inset\(50%\)$/.test(style.clipPath)) return true;
    }
    return false;
  };
  // Out of flow inside a control (a NavBadge over its icon, a positioned decoration): placed on
  // purpose, so not measured against the control.
  const outOfFlowWithin = (el: Element, stop: Element) => {
    for (let a: Element | null = el; a && a !== stop; a = a.parentElement) {
      const position = getComputedStyle(a).position;
      if (position === 'absolute' || position === 'fixed') return true;
    }
    return false;
  };
  const text = (el: Element) => ((el as HTMLElement).innerText || el.getAttribute('aria-label') || '').replace(/\s+/g, ' ').trim().slice(0, 60);
  const vw = document.documentElement.clientWidth;
  const round = (n: number) => Math.round(n * 10) / 10;

  // Sideways: the document, the page's scroller, elements past the window edge.
  // clientWidth, not innerWidth: a mobile browser zooms out to fit wide content, and innerWidth
  // (the visual viewport) grows with it.
  if (document.documentElement.scrollWidth > vw + 1)
    add({ check: 'page-overflow', detail: `document is ${document.documentElement.scrollWidth}px wide in a ${vw}px window` });
  const scroller = options.scroller ? document.querySelector(options.scroller) : null;
  if (scroller && scroller.scrollWidth > scroller.clientWidth + 1 && getComputedStyle(scroller).overflowX !== 'clip')
    add({ check: 'page-overflow', selector: describe(scroller), detail: `scroller content is ${scroller.scrollWidth}px wide in ${scroller.clientWidth}px` });
  const heldByScroller = (el: Element) => {
    for (let a = el.parentElement; a && a !== document.documentElement; a = a.parentElement) {
      const style = getComputedStyle(a);
      if (style.overflowX !== 'visible' && a !== scroller && a !== document.body) {
        const rect = a.getBoundingClientRect();
        if (rect.left >= -0.5 && rect.right <= vw + 0.5) return true;
      }
      if (style.position === 'fixed') break;
    }
    return false;
  };
  for (const el of document.body.querySelectorAll('*')) {
    const rect = el.getBoundingClientRect();
    if (rect.width === 0 || rect.height === 0 || (rect.right <= vw + 0.5 && rect.left >= -0.5)) continue;
    if (ignored(el) || heldByScroller(el) || !visible(el)) continue;
    // Report the outermost offender only.
    const parent = el.parentElement;
    if (parent && parent !== document.body) {
      const pr = parent.getBoundingClientRect();
      if ((pr.right > vw + 0.5 || pr.left < -0.5) && !heldByScroller(parent)) continue;
    }
    add({ check: 'wide-element', selector: describe(el), detail: `spans ${Math.round(rect.left)}..${Math.round(rect.right)} in a ${vw}px window` });
  }

  // Text that runs out of its control.
  const controlSelector = 'button, a[href], [role=button], [role=tab], [role=radio], [role=option], [role=menuitem], [role=link], summary, label, select' + (options.controls ? ', ' + options.controls : '');
  const truncated = (from: Element, stop: Element) => {
    for (let a: Element | null = from; a; a = a.parentElement) {
      const style = getComputedStyle(a);
      // An ellipsis only works on a block container: on a flex or grid box the text is just cut.
      if (style.textOverflow === 'ellipsis' && style.overflowX !== 'visible' && !/flex|grid/.test(style.display)) return true;
      if (style.webkitLineClamp && style.webkitLineClamp !== 'none') return true;
      if (a === stop) break;
    }
    return false;
  };
  const range = document.createRange();
  const controls = [...document.querySelectorAll(controlSelector)].filter((el) => !ignored(el) && visible(el));
  for (const control of controls) {
    const box = control.getBoundingClientRect();
    const walker = document.createTreeWalker(control, NodeFilter.SHOW_TEXT);
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
      const owner = node.parentElement;
      if (!node.nodeValue?.trim() || !owner || owner.closest(controlSelector) !== control || !visible(owner) || visuallyHidden(owner) || outOfFlowWithin(owner, control) || truncated(owner, control)) continue;
      range.selectNodeContents(node);
      const slack = Math.max(2.5, 0.3 * parseFloat(getComputedStyle(owner).fontSize));
      for (const line of range.getClientRects()) {
        if (line.width <= 0.5) continue;
        if (line.right > box.right + 1.5 || line.left < box.left - 1.5 || line.bottom > box.bottom + slack || line.top < box.top - slack) {
          add({ check: 'text-overflow', selector: describe(control), text: text(control), detail: `text ${Math.round(line.left)}..${Math.round(line.right)} x ${Math.round(line.top)}..${Math.round(line.bottom)} outside the control ${Math.round(box.left)}..${Math.round(box.right)} x ${Math.round(box.top)}..${Math.round(box.bottom)}` });
          break;
        }
      }
    }
    const style = getComputedStyle(control);
    if (control.clientWidth > 0 && control.scrollWidth > control.clientWidth + 1 && !/(auto|scroll)/.test(style.overflowX) && !truncated(control, control)) {
      // scrollWidth also counts absolutely positioned children and pseudo-elements (.hit-target's
      // ::after, a NavBadge): measure the in-flow content only.
      const inner = box.left + control.clientLeft;
      let right = inner + control.clientWidth;
      for (const child of control.querySelectorAll('*')) {
        if (outOfFlowWithin(child, control) || visuallyHidden(child)) continue;
        right = Math.max(right, child.getBoundingClientRect().right);
      }
      const textWalker = document.createTreeWalker(control, NodeFilter.SHOW_TEXT);
      for (let node = textWalker.nextNode(); node; node = textWalker.nextNode()) {
        const owner = node.parentElement;
        if (!node.nodeValue?.trim() || !owner || outOfFlowWithin(owner, control) || visuallyHidden(owner)) continue;
        range.selectNodeContents(node);
        right = Math.max(right, range.getBoundingClientRect().right);
      }
      if (right > inner + control.clientWidth + 1)
        add({ check: 'text-overflow', selector: describe(control), text: text(control), detail: `content ${Math.round(right - inner)}px wide in ${control.clientWidth}px` });
    }
  }

  // Numbers broken across lines ("1’234" | "’567", "12." | "11.2026").
  {
    const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
    const number = /[+\-−]?\d(?:[\d’'.,:/  -]*\d)?/g;
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
      const value = node.nodeValue ?? '';
      const owner = node.parentElement;
      if (!/\d/.test(value) || !owner || ignored(owner) || !visible(owner) || visuallyHidden(owner) || owner.closest('textarea, pre, code')) continue;
      range.selectNodeContents(node);
      if (range.getClientRects().length < 2) continue;
      for (const match of value.matchAll(number)) {
        if (match[0].length < 2) continue;
        range.setStart(node, match.index);
        range.setEnd(node, match.index + match[0].length);
        const tops = new Set([...range.getClientRects()].filter((r) => r.width > 0.5).map((r) => Math.round(r.top)));
        if (tops.size > 1) add({ check: 'number-split', selector: describe(owner), text: match[0], detail: `"${match[0]}" breaks across ${tops.size} lines` });
      }
    }
  }

  // Tap targets, with an invisible ::before/::after that grows the target. The pseudo-element's
  // box is measured against its containing block: the element itself when positioned (.hit-target),
  // else the nearest positioned ancestor (a stretched link's ::after with inset: 0 covers the card).
  const targetSelector = 'a[href], button, input:not([type=hidden]), select, textarea, summary, label, [role=button], [role=link], [role=tab], [role=checkbox], [role=radio], [role=switch], [role=menuitem], [role=option], [tabindex]:not([tabindex="-1"])' + (options.controls ? ', ' + options.controls : '');
  const containingBlock = (el: Element): Element => {
    for (let a: Element | null = el; a && a !== document.documentElement; a = a.parentElement) {
      const style = getComputedStyle(a);
      if (style.position !== 'static' || style.transform !== 'none' || style.translate !== 'none' || style.filter !== 'none' || /paint|layout|strict|content/.test(style.contain)) return a;
    }
    return document.documentElement;
  };
  const paddingBox = (el: Element) => {
    const rect = el.getBoundingClientRect();
    const style = getComputedStyle(el);
    return {
      left: rect.left + parseFloat(style.borderLeftWidth),
      right: rect.right - parseFloat(style.borderRightWidth),
      top: rect.top + parseFloat(style.borderTopWidth),
      bottom: rect.bottom - parseFloat(style.borderBottomWidth),
    };
  };
  const hitBox = (el: Element, rect: DOMRect) => {
    let left = rect.left, right = rect.right, top = rect.top, bottom = rect.bottom;
    for (const pseudo of ['::before', '::after']) {
      const style = getComputedStyle(el, pseudo);
      if (style.content === 'none' || style.content === 'normal' || style.position !== 'absolute' || style.display === 'none') continue;
      const px = (v: string) => (v.endsWith('px') ? parseFloat(v) : NaN);
      const [t, r, b, l] = [px(style.top), px(style.right), px(style.bottom), px(style.left)];
      if ([t, r, b, l].some(Number.isNaN)) continue;
      const block = paddingBox(containingBlock(el));
      left = Math.min(left, block.left + l);
      right = Math.max(right, block.right - r);
      top = Math.min(top, block.top + t);
      bottom = Math.max(bottom, block.bottom - b);
    }
    return { width: right - left, height: bottom - top };
  };
  const inlineInText = (el: Element) => {
    if (getComputedStyle(el).display !== 'inline' || !el.parentElement) return false;
    return [...el.parentElement.childNodes].some((n) => n.nodeType === Node.TEXT_NODE && !!n.nodeValue?.trim());
  };
  for (const el of document.querySelectorAll(targetSelector)) {
    if (ignored(el) || (el as HTMLButtonElement).disabled) continue;
    const rect = visible(el);
    if (!rect || rect.width <= 1 || rect.height <= 1) continue;
    const type = (el as HTMLInputElement).type;
    // A checkbox or radio takes the finger through its label, which is judged instead; the label of
    // a text field is just text (the field is the target).
    if (el.tagName === 'INPUT' && (type === 'checkbox' || type === 'radio') && ((el as HTMLInputElement).labels?.length ?? 0) > 0) continue;
    if (el.tagName === 'LABEL' && !['checkbox', 'radio'].includes(((el as HTMLLabelElement).control as HTMLInputElement | null)?.type ?? '')) continue;
    if (el.tagName === 'A' && inlineInText(el)) continue;
    const hit = hitBox(el, rect);
    if (hit.width < options.minTarget - 0.5 || hit.height < options.minTarget - 0.5)
      add({ check: 'tap-target', selector: describe(el), text: text(el), detail: `${round(hit.width)} x ${round(hit.height)} under ${options.minTarget} x ${options.minTarget}` });
  }

  // Field fonts (iOS zooms in under 16px) and small text.
  for (const el of document.querySelectorAll('input:not([type=checkbox], [type=radio], [type=range], [type=color], [type=file], [type=hidden], [type=button], [type=submit], [type=reset], [type=image]), select, textarea, [contenteditable=""], [contenteditable=true]')) {
    if (ignored(el) || !visible(el)) continue;
    const size = parseFloat(getComputedStyle(el).fontSize);
    if (size < options.minInputFont - 0.01) add({ check: 'input-font', selector: describe(el), detail: `font-size ${size}px under ${options.minInputFont}px` });
  }
  {
    const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
      const owner = node.parentElement;
      if (!node.nodeValue?.trim() || !owner || ignored(owner) || !visible(owner) || visuallyHidden(owner) || owner.closest('script, style, noscript, template')) continue;
      const size = parseFloat(getComputedStyle(owner).fontSize);
      if (size < options.minText - 0.01) add({ check: 'small-text', selector: describe(owner), text: text(owner), detail: `font-size ${size}px under ${options.minText}px` });
    }
  }
  return findings;
}

/** A focused field right after load: on a phone the keyboard opens and pwa-shell hides the nav. */
function focusedField(ignore: string): Finding[] {
  const el = document.activeElement;
  if (!el || el === document.body || (ignore && el.closest(ignore))) return [];
  const field = el.matches('input:not([type=checkbox], [type=radio], [type=range], [type=button], [type=submit], [type=reset]), select, textarea, [contenteditable=""], [contenteditable=true]');
  if (!field) return [];
  return [{ check: 'autofocus', selector: el.tagName.toLowerCase() + (el.id ? '#' + el.id : '') + (el.getAttribute('name') ? `[name="${el.getAttribute('name')}"]` : ''), detail: 'a field has focus after load: the keyboard opens and the nav goes' }];
}

interface CoverOptions {
  ignore: string;
  controls: string;
  scroller: string;
  maxSteps: number;
}

/**
 * Controls a finger cannot reach: at the centre and two inset corners of each visible control,
 * document.elementFromPoint must find the control, something inside it or around it (a label: its
 * control too). Two of three points on something else is a cover. The page is scrolled in steps
 * (the document, else its scroller or `main`) and a control counts only when it is covered at every
 * step it was on screen, so content passing under a sticky bar while scrolling is not reported, but
 * a row the nav hides at the end of the page is. Self-contained: Playwright serializes it.
 */
async function coveredTargets(options: CoverOptions): Promise<Finding[]> {
  const selector = 'button, a[href], [role=button], [role=tab], input:not([type=hidden]), select, textarea, summary, label[for]' + (options.controls ? ', ' + options.controls : '');
  const ignored = (el: Element) => !!options.ignore && !!el.closest(options.ignore);
  const describe = (el: Element): string => {
    const parts: string[] = [];
    for (let node: Element | null = el; node && node !== document.body && parts.length < 4; node = node.parentElement) {
      if (node.id) {
        parts.unshift(node.tagName.toLowerCase() + '#' + CSS.escape(node.id));
        break;
      }
      const classes = typeof node.className === 'string' ? node.className.trim().split(/\s+/).filter(Boolean).slice(0, 2) : [];
      let out = node.tagName.toLowerCase() + classes.map((c) => '.' + CSS.escape(c)).join('');
      const parent: Element | null = node.parentElement;
      if (parent) {
        const same = [...parent.children].filter((child) => child.tagName === node!.tagName);
        if (same.length > 1) out += `:nth-of-type(${same.indexOf(node) + 1})`;
      }
      parts.unshift(out);
    }
    return parts.join(' > ');
  };
  const label = (el: Element) => ((el as HTMLElement).innerText || el.getAttribute('aria-label') || el.getAttribute('title') || (el as HTMLInputElement).placeholder || '').replace(/\s+/g, ' ').trim().slice(0, 60);
  // An open modal dialog makes everything outside it inert.
  const modal = document.querySelector(':modal');
  const skipped = (el: Element) => {
    if (ignored(el) || (el as HTMLButtonElement).disabled || el.closest('[inert], [aria-hidden="true"]')) return true;
    if (modal && !modal.contains(el)) return true;
    // Inside a closed <details> or under content-visibility: hidden the box may keep a size, but
    // nothing of it is painted.
    if (el.checkVisibility && !el.checkVisibility({ opacityProperty: true, visibilityProperty: true })) return true;
    const style = getComputedStyle(el);
    return style.visibility === 'hidden' || style.display === 'none' || style.pointerEvents === 'none' || parseFloat(style.opacity) === 0;
  };
  // What a tap may land on: the control, inside it, around it; for a label its control, for a field its labels.
  const reaches = (el: Element, hit: Element) => {
    if (hit === el || el.contains(hit) || hit.contains(el)) return true;
    const control = el.tagName === 'LABEL' ? (el as HTMLLabelElement).control : null;
    if (control && (hit === control || control.contains(hit))) return true;
    for (const own of (el as HTMLInputElement).labels ?? []) if (own === hit || own.contains(hit)) return true;
    return false;
  };
  // The part of the element on screen: the window, cut by every clipping ancestor up to a fixed one.
  const onScreen = (el: Element) => {
    const r = el.getBoundingClientRect();
    let left = Math.max(r.left, 0), top = Math.max(r.top, 0);
    let right = Math.min(r.right, document.documentElement.clientWidth), bottom = Math.min(r.bottom, window.innerHeight);
    for (let a = el.parentElement; a && a !== document.documentElement; a = a.parentElement) {
      const style = getComputedStyle(a);
      if (style.overflowX !== 'visible' || style.overflowY !== 'visible') {
        const c = a.getBoundingClientRect();
        if (style.overflowX !== 'visible') { left = Math.max(left, c.left); right = Math.min(right, c.right); }
        if (style.overflowY !== 'visible') { top = Math.max(top, c.top); bottom = Math.min(bottom, c.bottom); }
      }
      if (style.position === 'fixed') break;
    }
    return right - left >= 4 && bottom - top >= 4 ? { left, top, right, bottom } : null;
  };
  const scrollable = (el: Element | null): el is HTMLElement => !!el && el.scrollHeight > el.clientHeight + 1 && /auto|scroll|overlay/.test(getComputedStyle(el).overflowY);
  const doc = document.scrollingElement ?? document.documentElement;
  const candidates = [options.scroller ? document.querySelector(options.scroller) : null, document.querySelector('main')];
  const pane: HTMLElement | null = doc.scrollHeight > window.innerHeight + 1 ? null : candidates.find(scrollable) ?? null;
  const box = pane ?? doc;
  const start = box.scrollTop;
  const frames = () => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve)));
  const quiet = async () => {
    await frames();
    const finite = document.getAnimations().filter((a) => a.playState === 'running' && a.effect?.getTiming().iterations !== Infinity);
    if (finite.length) await Promise.race([Promise.allSettled(finite.map((a) => a.finished)), new Promise((resolve) => setTimeout(resolve, 800))]);
    await frames();
  };

  const state = new Map<Element, { clear: boolean; finding?: Finding }>();
  const probe = (step: number) => {
    for (const el of document.querySelectorAll(selector)) {
      if (state.get(el)?.clear || skipped(el)) continue;
      const area = onScreen(el);
      if (!area) continue;
      const w = area.right - area.left, h = area.bottom - area.top;
      const ix = Math.min(10, w / 4), iy = Math.min(10, h / 4);
      const points = [[area.left + w / 2, area.top + h / 2], [area.left + ix, area.top + iy], [area.right - ix, area.bottom - iy]];
      const covers: Element[] = [];
      for (const [x, y] of points) {
        const hit = document.elementFromPoint(x, y);
        if (hit && !reaches(el, hit)) covers.push(hit);
      }
      if (covers.length < 2 || covers.some((c) => ignored(c))) {
        state.set(el, { clear: true });
        continue;
      }
      if (state.has(el)) continue;
      const cover = covers[0];
      const where = step === 0 ? 'on load' : `after scrolling ${Math.round(box.scrollTop)}px`;
      const coverText = label(cover);
      state.set(el, {
        clear: false,
        finding: {
          check: 'covered-target',
          selector: describe(el),
          text: label(el),
          detail: `${where}, a tap at ${Math.round(points[0][0])},${Math.round(points[0][1])} lands on ${describe(cover)}${coverText ? ` ("${coverText}")` : ''}`,
        },
      });
    }
  };

  await quiet();
  probe(0);
  const max = box.scrollHeight - box.clientHeight;
  const stride = Math.max(1, Math.round(box.clientHeight * 0.8));
  for (let step = 1; step <= options.maxSteps && box.scrollTop < max - 1; step++) {
    box.scrollTo({ top: Math.min(max, box.scrollTop + stride), behavior: 'instant' });
    await quiet();
    probe(step);
  }
  // Back where it was, and quiet again: a title that fades with the scroll must not be caught
  // half-way by the checks after this one.
  box.scrollTo({ top: start, behavior: 'instant' });
  await quiet();
  const findings: Finding[] = [];
  for (const entry of state.values()) if (!entry.clear && entry.finding) findings.push(entry.finding);
  return findings;
}

/** A console message, uncaught exception or failed request while a route was open. */
export interface PageError {
  kind: 'page-error' | 'console-error' | 'request-failed';
  text: string;
  /** The request's or the message's URL, where there is one. */
  url?: string;
}

/**
 * Collects page errors from now on: uncaught exceptions (`pageerror`), console messages of type
 * error and failed requests. Call before page.goto; the returned function gives the findings so far
 * (checks `page-error`, `console-error`, `request-failed`). Not counted: requests the page aborted
 * itself (net::ERR_ABORTED: a navigation, a cancelled fetch) and whatever `allow` accepts, the
 * application's documented noise (a request its mocks fail on purpose, a third-party warning).
 */
export function watchErrors(page: Page, allow?: (error: PageError) => boolean): () => Finding[] {
  const errors: PageError[] = [];
  page.on('pageerror', (error) => errors.push({ kind: 'page-error', text: String(error?.stack ?? error).split('\n').slice(0, 3).join(' | ') }));
  page.on('console', (message) => {
    if (message.type() !== 'error') return;
    errors.push({ kind: 'console-error', text: message.text(), url: message.location()?.url || undefined });
  });
  page.on('requestfailed', (request) => {
    const failure = request.failure()?.errorText ?? 'failed';
    if (/ERR_ABORTED|NS_BINDING_ABORTED|cancelled/i.test(failure)) return;
    errors.push({ kind: 'request-failed', text: `${request.method()} ${failure}`, url: request.url() });
  });
  return () => errors
    .filter((error) => !allow?.(error))
    .map((error) => ({ check: error.kind, detail: error.text.slice(0, 300) + (error.url ? ` (${error.url})` : '') }));
}

async function layoutShift(page: Page, options: AuditOptions): Promise<Finding[]> {
  const supported = await page.evaluate(() => {
    const store = (window as unknown as { __phoneAuditShifts?: { supported: boolean; entries: unknown[] } }).__phoneAuditShifts;
    if (!store) return null;
    store.entries.length = 0;
    return store.supported;
  });
  if (supported === null) throw new Error('phone audit: call installLayoutShiftObserver(page) before page.goto, or skip "layout-shift"');
  if (!supported) return [];
  const refetch = options.refetch ?? ((p: Page) => p.evaluate(() => {
    document.dispatchEvent(new Event('visibilitychange', { bubbles: true }));
    window.dispatchEvent(new Event('focus'));
  }));
  await refetch(page);
  await page.waitForTimeout(options.refetchWindowMs ?? 1500);
  await page.waitForLoadState('networkidle', { timeout: 5000 }).catch(() => {});
  const entries = await page.evaluate(() => (window as unknown as { __phoneAuditShifts: { entries: { value: number; sources: string[] }[] } }).__phoneAuditShifts.entries.slice());
  const score = entries.reduce((sum, entry) => sum + entry.value, 0);
  if (score <= (options.maxLayoutShift ?? 0.001)) return [];
  const sources = [...new Set(entries.flatMap((entry) => entry.sources))].slice(0, 6);
  return [{ check: 'layout-shift', selector: sources.join(', ') || undefined, detail: `layout shifted by ${score.toFixed(4)} during the refetch window` }];
}

async function sidewaysPan(page: Page, options: AuditOptions): Promise<Finding[]> {
  let cdp: CDPSession;
  try {
    cdp = await page.context().newCDPSession(page);
  } catch {
    return []; // not Chromium
  }
  const scroller = options.scroller ?? '.app-shell-main';
  const state = () => page.evaluate((selector) => ({
    url: location.pathname + location.search + location.hash,
    x: window.scrollX,
    vv: window.visualViewport ? Math.round(window.visualViewport.offsetLeft * 10) / 10 : 0,
    scale: window.visualViewport?.scale ?? 1,
    main: (selector && document.querySelector(selector)?.scrollLeft) || 0,
  }), scroller);
  const size = page.viewportSize() ?? { width: 390, height: 844 };
  const swipe = async (x0: number, x1: number, y: number) => {
    await cdp.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x: x0, y }] });
    for (let step = 1; step <= 12; step++) {
      await cdp.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: [{ x: x0 + ((x1 - x0) * step) / 12, y }] });
      await page.waitForTimeout(16);
    }
    await cdp.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
    await page.waitForTimeout(400);
  };
  const findings: Finding[] = [];
  const before = await state();
  const href = page.url();
  // Swipe across a few rows of the page (upper content, middle, lower), both ways.
  for (const y of [0.35, 0.6, 0.8].map((f) => Math.round(size.height * f))) {
    for (const [from, to] of [[0.85, 0.15], [0.15, 0.85]]) {
      await swipe(Math.round(size.width * from), Math.round(size.width * to), y);
      const after = await state();
      const way = from > to ? 'left' : 'right';
      if (after.url !== before.url) {
        // A history swipe (sideways overscroll handed to the browser) or a swipe handler.
        findings.push({ check: 'sideways-pan', detail: `a swipe ${way} at y=${y} navigated to ${after.url}` });
        await page.goto(href, { waitUntil: 'domcontentloaded' }).catch(() => {});
        await settle(page);
      } else if (after.x || after.vv || after.main || Math.abs(after.scale - before.scale) > 0.01) {
        findings.push({ check: 'sideways-pan', detail: `a swipe ${way} at y=${y} moved the page: scrollX ${after.x}, visual viewport ${after.vv}, scroller ${after.main}, scale ${after.scale}` });
      }
      await page.evaluate((selector) => {
        window.scrollTo(0, window.scrollY);
        const el = selector ? document.querySelector(selector) : null;
        if (el) el.scrollLeft = 0;
      }, scroller);
    }
  }
  await cdp.detach().catch(() => {});
  return findings.slice(0, 3);
}

async function axe(page: Page, options: AuditOptions): Promise<Finding[]> {
  if (options.axe === false) return [];
  const module = await import('@axe-core/playwright');
  const Builder = (module as unknown as { AxeBuilder?: typeof module.default }).AxeBuilder ?? module.default;
  let builder = new Builder({ page }).withTags(options.axe?.tags ?? WCAG_TAGS);
  if (options.axe?.disableRules?.length) builder = builder.disableRules(options.axe.disableRules);
  if (options.ignore) builder = builder.exclude(options.ignore);
  const result = await builder.analyze();
  return result.violations.flatMap((violation) => violation.nodes.map((node) => ({
    check: 'axe',
    selector: node.target.map(String).join(' '),
    detail: `${violation.id} (${violation.impact ?? 'n/a'}): ${violation.help}`,
  })));
}

/**
 * Audits the loaded page (open it with openRoute after installLayoutShiftObserver). Order matters:
 * the focus check runs first, the swipe last (it may move or navigate the page).
 */
export async function auditPage(page: Page, options: AuditOptions = {}): Promise<Finding[]> {
  const skip = new Set(options.skip ?? []);
  const findings: Finding[] = [];
  if (!skip.has('autofocus')) findings.push(...await page.evaluate(focusedField, options.ignore ?? ''));
  findings.push(...await page.evaluate(domChecks, {
    ignore: options.ignore ?? '',
    controls: options.controls ?? '',
    scroller: options.scroller ?? '.app-shell-main',
    minTarget: options.minTarget ?? 44,
    minInputFont: options.minInputFont ?? 16,
    minText: options.minText ?? 14,
    skip: [...skip],
  }));
  if (!skip.has('axe')) findings.push(...await axe(page, options));
  for (const rule of options.platformRules ?? []) if (!skip.has(rule.name)) findings.push(...await rule.run(page));
  // It scrolls the page: after the checks that read the page as loaded.
  if (!skip.has('covered-target')) findings.push(...await page.evaluate(coveredTargets, {
    ignore: options.ignore ?? '',
    controls: options.controls ?? '',
    scroller: options.scroller ?? '.app-shell-main',
    maxSteps: options.coverSteps ?? 8,
  }));
  if (!skip.has('layout-shift')) findings.push(...await layoutShift(page, options));
  if (!skip.has('sideways-pan')) findings.push(...await sidewaysPan(page, options));
  return findings;
}

/**
 * With reducedMotion 'reduce' on the context: after load (network idle, fonts, two frames, no
 * waiting for animations), nothing may still be animating or transitioning.
 */
export async function auditReducedMotion(page: Page, options: Pick<AuditOptions, 'ignore'> = {}): Promise<Finding[]> {
  return page.evaluate((ignore) => {
    const findings: Finding[] = [];
    for (const animation of document.getAnimations()) {
      if (animation.playState !== 'running') continue;
      const target = (animation.effect as KeyframeEffect | null)?.target;
      if (target && ignore && target.closest(ignore)) continue;
      const name = (animation as CSSAnimation).animationName ?? (animation as CSSTransition).transitionProperty ?? animation.id ?? 'script';
      const where = target ? target.tagName.toLowerCase() + (target.id ? '#' + target.id : '') + (typeof target.className === 'string' && target.className.trim() ? '.' + target.className.trim().split(/\s+/).slice(0, 2).join('.') : '') : undefined;
      findings.push({ check: 'running-animation', selector: where, detail: `"${name || 'script'}" still runs under prefers-reduced-motion: reduce` });
    }
    return findings;
  }, options.ignore ?? '');
}

/**
 * A platform rule that forbids a computed style: every visible element whose `property` is not
 * one of `allowed` is a finding. Example: forbidComputedStyle('no-glass', 'backdrop-filter').
 */
export function forbidComputedStyle(name: string, property: string, allowed: string[] = ['none'], ignore = ''): PlatformRule {
  return {
    name,
    run: (page) => page.evaluate(({ name, property, allowed, ignore }) => {
      const findings: Finding[] = [];
      for (const el of document.querySelectorAll('*')) {
        if (ignore && el.closest(ignore)) continue;
        const value = getComputedStyle(el).getPropertyValue(property).trim();
        if (!value || allowed.includes(value)) continue;
        const rect = el.getBoundingClientRect();
        if (rect.width < 1 || rect.height < 1) continue;
        const classes = typeof el.className === 'string' ? el.className.trim().split(/\s+/).filter(Boolean).slice(0, 2) : [];
        findings.push({ check: name, selector: el.tagName.toLowerCase() + (el.id ? '#' + el.id : '') + classes.map((c) => '.' + c).join(''), detail: `${property}: ${value}` });
      }
      return findings;
    }, { name, property, allowed, ignore }),
  };
}

/** One line per finding, for a test failure message. */
export function formatFindings(findings: Finding[]): string {
  if (findings.length === 0) return 'no findings';
  return `${findings.length} phone-audit finding(s):\n` + findings.map((f) => `  [${f.check}] ${f.selector ? f.selector + ': ' : ''}${f.detail}${f.text ? ` ("${f.text}")` : ''}`).join('\n');
}
