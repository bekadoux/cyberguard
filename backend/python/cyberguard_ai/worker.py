"""One-at-a-time CPU-bounded polling worker. No in-process task queue."""
import asyncio
import logging
import signal
from contextlib import suppress

import httpx
from .config import Settings
from .db import Claim, Storage
from .go_client import ContextUnavailable, job_context
from .models import ModelUnavailable, run_assessment

log = logging.getLogger("cyberguard.ai.worker")


async def keep_lease(storage: Storage, claim: Claim, lease: int, stop: asyncio.Event):
    while not stop.is_set():
        try:
            await asyncio.wait_for(stop.wait(), timeout=max(10, lease // 3))
            return
        except asyncio.TimeoutError:
            if not await asyncio.to_thread(storage.renew, claim.job_id, claim.claim_token, lease):
                log.warning("Job lease lost: job_id=%s", claim.job_id)
                return


async def execute_claim(claim: Claim, cfg: Settings, storage: Storage,
                        http: httpx.AsyncClient) -> bool:
    stop_lease = asyncio.Event()
    task = asyncio.create_task(keep_lease(storage, claim, cfg.lease_seconds, stop_lease))
    try:
        context = await job_context(claim.job_id, cfg, http)
        if context.incident_id != claim.incident_id or context.incident_revision != claim.incident_revision:
            raise ContextUnavailable("stale_context")
        assessment = await run_assessment(context, cfg, http)
        return await asyncio.to_thread(storage.finish, claim.job_id, claim.claim_token,
                                       assessment.model_dump(mode="json"),
                                       "fake" if assessment.is_fake else "kev_llm")
    except ContextUnavailable as exc:
        log.warning("Context failed: job_id=%s code=%s", claim.job_id, exc.code)
        await asyncio.to_thread(storage.fail, claim.job_id, claim.claim_token, exc.code)
        return False
    except ModelUnavailable as exc:
        # Never silently generate a fake result after inference failure.
        code = str(exc)
        code = code if code in ("kev_unavailable_or_invalid", "llm_unavailable_or_invalid", "approved_playbook_missing") else "model_unavailable"
        log.warning("Inference failed: job_id=%s code=%s", claim.job_id, code)
        await asyncio.to_thread(storage.fail, claim.job_id, claim.claim_token, code)
        return False
    except Exception:
        # Do not log incident body, secrets, model output or exception details.
        log.error("Unexpected AI job failure: job_id=%s", claim.job_id)
        with suppress(Exception):
            await asyncio.to_thread(storage.fail, claim.job_id, claim.claim_token, "internal_error")
        return False
    finally:
        stop_lease.set()
        await task


async def worker_loop(cfg: Settings, storage: Storage, stop: asyncio.Event):
    async with httpx.AsyncClient(trust_env=False) as http:
        while not stop.is_set():
            try:
                claim = await asyncio.to_thread(storage.claim, cfg.max_attempts, cfg.lease_seconds)
                if claim:
                    success = await execute_claim(claim, cfg, storage, http)
                    log.info("Job processed: job_id=%s completed=%s", claim.job_id, success)
                    continue
            except Exception:
                log.error("AI queue temporarily unavailable")
            try:
                await asyncio.wait_for(stop.wait(), cfg.poll_seconds)
            except asyncio.TimeoutError:
                pass


def main():
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s %(message)s")
    cfg = Settings.from_env()
    stop = asyncio.Event()
    loop = asyncio.new_event_loop()
    asyncio.set_event_loop(loop)
    for sig in (signal.SIGINT, signal.SIGTERM):
        with suppress(NotImplementedError):
            loop.add_signal_handler(sig, stop.set)
    try:
        loop.run_until_complete(worker_loop(cfg, Storage(cfg.database_url), stop))
    finally:
        loop.close()


if __name__ == "__main__":
    main()
