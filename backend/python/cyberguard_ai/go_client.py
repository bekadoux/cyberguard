from uuid import UUID

import httpx

from .config import Settings
from .schemas import IncidentContext


class ContextUnavailable(RuntimeError):
    def __init__(self, code: str):
        self.code = code
        super().__init__(code)


async def job_context(job_id, cfg: Settings, http: httpx.AsyncClient) -> IncidentContext:
    try:
        response = await http.get(
            f'{cfg.go_base_url}/internal/ai/jobs/{job_id}/context',
            headers={'Authorization': f'Bearer {cfg.internal_token}'},
            timeout=10,
        )
        if response.status_code == 409:
            raise ContextUnavailable('stale_context')
        if response.status_code != 200:
            raise ContextUnavailable('go_context_unavailable')
        return IncidentContext.model_validate(response.json())
    except (httpx.HTTPError, ValueError) as exc:
        raise ContextUnavailable('go_context_unavailable') from exc


async def authorize_analyst(
    incident_id: UUID, cookie: str, cfg: Settings, http: httpx.AsyncClient
) -> int:
    """Go remains the sole session/incident authorization authority.

    Both checks are repeated on every read. Reporters cannot read AI output on
    their own incidents, including analysts and admins. Malformed Go data fails closed.
    """
    if not cookie:
        return 401
    headers = {'Cookie': cookie}
    try:
        me = await http.get(f'{cfg.go_base_url}/auth/me', headers=headers, timeout=10)
        if me.status_code in (401, 403):
            return 401
        if me.status_code != 200:
            return 503
        user = me.json()['user']
        actor_id = UUID(user['id'])
        role = user['role']
        if role not in ('analyst', 'admin'):
            return 403
        detail = await http.get(
            f'{cfg.go_base_url}/incidents/{incident_id}', headers=headers, timeout=10
        )
        if detail.status_code == 200:
            incident = detail.json()['incident']
            if UUID(incident['id']) != incident_id:
                return 503
            if UUID(incident['reporter_id']) == actor_id:
                return 403
            return 200
        if detail.status_code == 401:
            return 401
        if detail.status_code in (403, 404):
            return 404
        return 503
    except (httpx.HTTPError, ValueError, KeyError, TypeError, AttributeError):
        return 503
