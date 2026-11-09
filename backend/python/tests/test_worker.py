import asyncio
from uuid import UUID
import httpx
from cyberguard_ai.worker import execute_claim
from cyberguard_ai.db import Claim
from cyberguard_ai.config import Settings

JOB = UUID("22222222-2222-4222-8222-222222222222")
INCIDENT = UUID("11111111-1111-4111-8111-111111111111")
TOKEN = UUID("33333333-3333-4333-8333-333333333333")


class FakeStorage:
    def __init__(self):
        self.completed = None
        self.failed = None

    def finish(self, job, token, data, source):
        self.completed = (job, token, data, source)
        return True

    def fail(self, job, token, code):
        self.failed = (job, token, code)
        return True

    def renew(self, *_args):
        return True


def test_worker_fake_job_persists_clear_provenance():
    cfg = Settings(database_url="unused", go_base_url="http://go", internal_token="secret", mode="fake")
    storage = FakeStorage()
    claim = Claim(JOB, INCIDENT, 1, TOKEN)
    def handle(req):
        assert req.headers.get("Authorization") == "Bearer secret"
        return httpx.Response(200, json={
            "job_id": str(JOB), "incident_id": str(INCIDENT), "incident_revision": 1,
            "title": "Test", "description": "Demo synthetic report", "affected_system": None,
        })
    async def run():
        async with httpx.AsyncClient(transport=httpx.MockTransport(handle)) as http:
            return await execute_claim(claim, cfg, storage, http)
    assert asyncio.run(run())
    assert storage.completed[3] == "fake"
    assert storage.completed[2]["is_fake"]
    assert storage.failed is None


def test_worker_marks_stale_context_failed():
    cfg = Settings(database_url="unused", go_base_url="http://go", internal_token="secret", mode="fake")
    storage = FakeStorage()
    async def run():
        async with httpx.AsyncClient(transport=httpx.MockTransport(lambda _: httpx.Response(409))) as http:
            return await execute_claim(Claim(JOB, INCIDENT, 1, TOKEN), cfg, storage, http)
    assert not asyncio.run(run())
    assert storage.completed is None
    assert storage.failed[2] == "stale_context"
