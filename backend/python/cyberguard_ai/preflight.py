"""Non-mutating deployment probes; never claim jobs or print sensitive responses."""

import asyncio
import sys

import httpx

from .config import Settings
from .db import Storage


async def dependencies_ready(cfg: Settings, http: httpx.AsyncClient) -> bool:
    """Gate each claim, including after reboot and during model recovery.

    Probes establish availability, not inference quality or an atomic guarantee that
    a dependency will remain available for the following job.
    """
    try:
        response = await http.get(f'{cfg.go_base_url}/readyz', timeout=5)
        response.raise_for_status()
        if cfg.mode == 'fake':
            return True
        response = await http.get(f'{cfg.kev_base_url}/v1/models', timeout=5)
        response.raise_for_status()
        models = response.json()['models']
        if not any(model.get('name') == cfg.kev_model for model in models):
            return False
        response = await http.get(f'{cfg.llm_base_url}/health', timeout=5)
        response.raise_for_status()
        return response.json().get('status') == 'ok'
    except (httpx.HTTPError, ValueError, KeyError, TypeError, AttributeError):
        return False


async def check() -> bool:
    """Check storage and service availability without consuming queued work."""
    cfg = Settings.from_env()
    await asyncio.to_thread(Storage(cfg.database_url).ping)
    async with httpx.AsyncClient(trust_env=False, verify=cfg.http_verify()) as http:
        return await dependencies_ready(cfg, http)


def main() -> None:
    """Expose safe deployment diagnostics without exception/credential details."""
    try:
        ready = asyncio.run(check())
    except Exception:
        ready = False
    print(
        'Application ready.'
        if ready
        else 'Application not ready; check configuration and dependencies.'
    )
    sys.exit(0 if ready else 1)


if __name__ == '__main__':
    main()
