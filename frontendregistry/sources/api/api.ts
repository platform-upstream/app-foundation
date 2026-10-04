export type ErrorEnvelope = { error: string };

export class APIError extends Error {
  readonly status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = 'APIError';
    this.status = status;
  }
}

export type RuntimeSchema<T> = { parse(value: unknown): T };

export type JSONClientOptions = {
  baseURL?: URL;
  fallbackMessage?: string;
  onAuthRequired?: (response: Response) => void | Promise<void>;
  isAuthRequired?: (response: Response) => boolean;
};

export type JSONRequest = <T>(
  path: string,
  schema: RuntimeSchema<T>,
  init?: RequestInit,
) => Promise<T>;

function defaultAuthRequired(response: Response): boolean {
  return response.type === 'opaqueredirect';
}

function errorMessage(value: unknown, fallback: string): string {
  if (typeof value !== 'object' || value === null || !('error' in value)) return fallback;
  const message = (value as ErrorEnvelope).error;
  return typeof message === 'string' && message !== '' ? message : fallback;
}

export function createJSONClient(options: JSONClientOptions = {}): JSONRequest {
  const baseURL = options.baseURL ?? new URL('api/', document.baseURI);
  const fallback = options.fallbackMessage ?? 'The action could not be completed.';
  const isAuthRequired = options.isAuthRequired ?? defaultAuthRequired;

  if (baseURL.origin !== location.origin) throw new Error('API base URL must be same-origin');

  return async function request<T>(path: string, schema: RuntimeSchema<T>, init: RequestInit = {}): Promise<T> {
    const target = new URL(path.replace(/^\/+/, ''), baseURL);
    if (target.origin !== baseURL.origin || !target.pathname.startsWith(baseURL.pathname)) {
      throw new Error('API path escapes the configured base URL');
    }

    const headers = new Headers(init.headers);
    if (init.body !== undefined && init.body !== null && !headers.has('Content-Type')) {
      headers.set('Content-Type', 'application/json');
    }
    const response = await fetch(target, { ...init, headers, redirect: 'manual', credentials: 'same-origin' });
    if (isAuthRequired(response)) {
      await options.onAuthRequired?.(response);
      throw new APIError(401, 'Authentication required.');
    }

    let raw: unknown;
    try {
      raw = await response.json();
    } catch {
      raw = undefined;
    }
    if (!response.ok) throw new APIError(response.status, errorMessage(raw, fallback));
    return schema.parse(raw);
  };
}
