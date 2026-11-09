import { ApiError } from '../../shared/api/client';
import { authApi, type Credentials, type User } from './api';

type SessionState = {
  status: 'checking' | 'anonymous' | 'authenticated' | 'unavailable';
  user: User | null;
  pending: boolean;
  error: ApiError | null;
};

/** One store per application; identity stays in memory, never browser storage. */
export class SessionStore {
  private state: SessionState = {
    status: 'checking',
    user: null,
    pending: false,
    error: null,
  };
  private listeners = new Set<() => void>();
  private generation = 0;
  private refreshController: AbortController | null = null;

  constructor(private readonly api = authApi) {}

  getSnapshot = (): SessionState => this.state;

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };

  private update(patch: Partial<SessionState>): void {
    this.state = { ...this.state, ...patch };
    this.listeners.forEach((listener) => listener());
  }

  cancelRefresh = (): void => {
    this.generation++;
    this.refreshController?.abort();
    this.refreshController = null;
  };

  refresh = async (): Promise<void> => {
    if (this.state.pending) return;
    this.cancelRefresh();
    const generation = this.generation;
    if (this.state.status === 'unavailable') {
      this.update({ status: 'checking', error: null });
    }
    const controller = new AbortController();
    this.refreshController = controller;
    try {
      const user = await this.api.me(controller.signal);
      if (generation === this.generation) {
        this.update({ status: 'authenticated', user, error: null });
      }
    } catch (error) {
      if (generation !== this.generation) return;
      const failure = asApiError(error);
      if (failure.status === 401) {
        this.update({ status: 'anonymous', user: null, error: null });
      } else {
        // Temporary failure is not proof of logout. Keep identity, but gate access.
        this.update({ status: 'unavailable', error: failure });
      }
    } finally {
      if (generation === this.generation) this.refreshController = null;
    }
  };

  private async mutate(action: () => Promise<User | null>): Promise<void> {
    // Reject overlaps instead of queuing a password or issuing competing cookie writes.
    if (this.state.pending) throw new ApiError(0, 'auth_pending');
    this.cancelRefresh();
    this.update({ pending: true, error: null });
    try {
      const user = await action();
      this.update({ status: user ? 'authenticated' : 'anonymous', user, error: null });
    } catch (error) {
      throw asApiError(error);
    } finally {
      this.update({ pending: false });
    }
  }

  login = (credentials: Credentials): Promise<void> =>
    this.mutate(() => this.api.login(credentials));

  logout = (): Promise<void> =>
    this.mutate(async () => {
      await this.api.logout();
      return null;
    });
}

function asApiError(error: unknown): ApiError {
  return error instanceof ApiError ? error : new ApiError(0, 'network_unavailable');
}
