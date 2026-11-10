export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly retryAt: number | null;
  readonly requestId: string | null;

  constructor(status: number, code: string, headers = new Headers()) {
    // Never propagate raw response bodies, credentials or backend diagnostics.
    super(code);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
    this.requestId = headers.get('X-Request-ID');
    const retry = headers.get('Retry-After');
    const delay = retry && /^\d+$/.test(retry) ? Number(retry) * 1000 : NaN;
    const deadline = retry ? Date.parse(retry) : NaN;
    this.retryAt = Number.isFinite(delay)
      ? Date.now() + delay
      : Number.isFinite(deadline)
        ? Math.max(Date.now(), deadline)
        : null;
  }
}

type RequestOptions = {
  method?: 'GET' | 'POST' | 'PATCH';
  body?: unknown;
  signal?: AbortSignal;
  expectedStatus?: number;
  service?: 'go' | 'ai';
};

export async function request(
  path: string,
  options: RequestOptions = {},
): Promise<unknown> {
  const headers = new Headers({ Accept: 'application/json' });
  if (options.body !== undefined) headers.set('Content-Type', 'application/json');
  const timeout = AbortSignal.timeout(15_000);
  let response: Response;
  try {
    response = await fetch(`${options.service === 'ai' ? '/ai' : '/api'}${path}`, {
      method: options.method ?? 'GET',
      credentials: 'include',
      cache: 'no-store',
      headers,
      body: options.body === undefined ? undefined : JSON.stringify(options.body),
      signal: options.signal ? AbortSignal.any([options.signal, timeout]) : timeout,
    });
  } catch (error) {
    if (options.signal?.aborted) throw error;
    throw new ApiError(0, 'network_unavailable');
  }

  if (
    response.ok &&
    options.expectedStatus !== undefined &&
    response.status !== options.expectedStatus
  ) {
    throw new ApiError(response.status, 'invalid_response', response.headers);
  }
  if (response.status === 204) return undefined;
  const data: unknown = await response.json().catch(() => undefined);
  if (!response.ok) {
    const envelope = data as { error?: { code?: unknown } } | undefined;
    const code = envelope?.error?.code;
    throw new ApiError(
      response.status,
      typeof code === 'string' ? code : 'request_failed',
      response.headers,
    );
  }
  if (data === undefined) throw new ApiError(response.status, 'invalid_response');
  return data;
}
