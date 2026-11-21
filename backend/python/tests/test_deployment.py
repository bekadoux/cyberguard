"""Deployment trust and readiness regressions; no live services or model weights."""

import asyncio
import ssl
from dataclasses import replace
from uuid import UUID

import httpx
import pytest
from cyberguard_ai import preflight, worker
from cyberguard_ai.config import Settings
from cyberguard_ai.db import Claim
from cyberguard_ai.db import Storage as DatabaseStorage
from cyberguard_ai.preflight import dependencies_ready


def settings(**kwargs):
    return Settings(
        database_url='unused', go_base_url='https://go', internal_token='test', **kwargs
    )


def test_explicit_ca_keeps_certificate_and_hostname_verification(monkeypatch):
    context = ssl.create_default_context()
    calls = []

    def create(*, cafile):
        calls.append(cafile)
        return context

    monkeypatch.setattr(ssl, 'create_default_context', create)
    assert settings(go_ca_file='/run/certs/ca.crt').http_verify() is context
    assert calls == ['/run/certs/ca.crt']
    assert context.verify_mode == ssl.CERT_REQUIRED
    assert context.check_hostname
    assert settings().http_verify() is True


@pytest.mark.parametrize('setting', ['AI_REQUIRE_TLS', 'AI_CHECK_DEPENDENCIES'])
def test_invalid_boolean_fails_closed(monkeypatch, setting):
    monkeypatch.setenv(setting, 'ture')
    with pytest.raises(ValueError, match=setting):
        Settings.from_env()


def test_tls_policy_rejects_http_and_insecure_database_without_leaking_secrets(tmp_path):
    cfg = settings(require_tls=True)
    with pytest.raises(ValueError, match='HTTPS'):
        replace(cfg, go_base_url='http://go').validate_tls()
    private_url = 'postgres://python_incidents:DO_NOT_PRINT@db/incidents?sslmode=disable'
    with pytest.raises(ValueError, match='verify-full') as error:
        replace(cfg, database_url=private_url).validate_tls()
    assert 'DO_NOT_PRINT' not in str(error.value)
    with pytest.raises(ValueError, match='Invalid AI_DATABASE_URL') as error:
        replace(cfg, database_url='DO_NOT_PRINT invalid connection').validate_tls()
    assert 'DO_NOT_PRINT' not in str(error.value)
    ca_file = tmp_path / 'ca.crt'
    ca_file.write_text('Test checks database policy; HTTP context mocked separately.')
    replace(
        cfg,
        database_url=(
            'postgres://python_incidents@db/cyberguard_incidents'
            f'?sslmode=verify-full&sslrootcert={ca_file}'
        ),
    ).validate_tls()
    with pytest.raises(ValueError, match='requires python_incidents'):
        replace(
            cfg,
            database_url=(
                'postgres://bootstrap_admin@db/cyberguard_incidents'
                f'?sslmode=verify-full&sslrootcert={ca_file}'
            ),
        ).validate_tls()


@pytest.mark.parametrize('unavailable', ['auth', 'go', 'kev', 'llm', None])
def test_dependency_probes_require_all_services(unavailable):
    cfg = settings(kev_base_url='http://kev', llm_base_url='http://llm')

    def handle(request):
        if request.url.path == '/internal/ai/auth-check':
            assert request.url.host == 'go'
            assert request.headers['Authorization'] == 'Bearer test'
            return httpx.Response(401 if unavailable == 'auth' else 204)
        assert 'Authorization' not in request.headers
        if request.url.host == unavailable:
            return httpx.Response(503)
        if request.url.host == 'kev':
            return httpx.Response(200, json={'models': [{'name': 'kev-latest'}]})
        return httpx.Response(200, json={'status': 'ok'})

    async def run():
        async with httpx.AsyncClient(transport=httpx.MockTransport(handle)) as client:
            return await dependencies_ready(cfg, client)

    assert asyncio.run(run()) is (unavailable is None)


@pytest.mark.parametrize(
    'body', [{}, {'models': None}, {'models': [None]}, {'models': []}]
)
def test_invalid_model_readiness_fails_closed(body):
    def handle(request):
        if request.url.path == '/internal/ai/auth-check':
            return httpx.Response(204)
        return httpx.Response(200, json=body if request.url.host == 'kev' else {})

    async def run():
        async with httpx.AsyncClient(transport=httpx.MockTransport(handle)) as client:
            return await dependencies_ready(settings(kev_base_url='http://kev'), client)

    assert not asyncio.run(run())


def test_worker_does_not_claim_until_dependencies_recover(monkeypatch):
    stop = asyncio.Event()
    observations = []
    ready = iter([False, True, False, True])

    async def probe(*_args, **_kwargs):
        value = next(ready)
        observations.append(value)
        return value

    class Storage(DatabaseStorage):
        claims = 0

        def __init__(self):
            super().__init__('unused')

        def worker_ready(self) -> bool:
            return True

        def claim(self, attempts: int, lease: int) -> None:
            assert observations[-1] is True
            self.claims += 1
            if self.claims == 2:
                stop.set()

    monkeypatch.setattr(worker, 'dependencies_ready', probe)
    storage = Storage()
    asyncio.run(
        worker.worker_loop(
            settings(check_dependencies=True, poll_seconds=0.001), storage, stop
        )
    )
    assert observations == [False, True, False, True]
    assert storage.claims == 2


@pytest.mark.parametrize('unavailable', [False, True])
def test_worker_preserves_queue_until_snapshot_safety_is_ready(monkeypatch, unavailable):
    stop = asyncio.Event()
    calls = []

    async def probe(*_args, **_kwargs):
        return True

    class Storage(DatabaseStorage):
        def __init__(self):
            super().__init__('unused')

        def worker_ready(self) -> bool:
            calls.append('readiness')
            stop.set()
            if unavailable:
                raise RuntimeError('missing safety migration')
            return False

        def claim(self, attempts: int, lease: int) -> None:
            raise AssertionError('Unsafe queue must not be claimed')

    monkeypatch.setattr(worker, 'dependencies_ready', probe)
    asyncio.run(worker.worker_loop(settings(check_dependencies=True), Storage(), stop))
    assert calls == ['readiness']


def test_preflight_rejects_pending_legacy_jobs_before_http_probes(monkeypatch):
    class Storage:
        def __init__(self, _url):
            pass

        def worker_ready(self) -> bool:
            return False

    monkeypatch.setattr(preflight, 'Storage', Storage)
    monkeypatch.setattr(Settings, 'from_env', classmethod(lambda cls: settings()))
    assert not asyncio.run(preflight.check())


@pytest.mark.parametrize('denial', [401, 403])
@pytest.mark.parametrize('check_services', [False, True])
def test_worker_auth_fault_preserves_claim_and_blocks_queue(
    monkeypatch, denial, check_services
):
    """Exercise startup mismatch, rotation between probe and read, then recovery."""
    stop = asyncio.Event()
    events = []
    probe_statuses = iter([denial, 204, denial, 200, 204])
    job = Claim(UUID(int=1), UUID(int=2), 1, UUID(int=3))

    def respond(request):
        if request.url.path == '/internal/ai/auth-check':
            status = next(probe_statuses)
            events.append(('probe', status))
            assert request.headers['Authorization'] == 'Bearer test'
            return httpx.Response(status)
        if request.url.path == '/readyz' and check_services:
            return httpx.Response(200)
        assert request.url.path == f'/internal/ai/jobs/{job.job_id}/context'
        events.append(('context', denial))
        return httpx.Response(denial)

    class Storage(DatabaseStorage):
        def worker_ready(self) -> bool:
            return True

        def claim(self, attempts: int, lease: int) -> Claim | None:
            events.append('claim')
            if events.count('claim') == 1:
                return job
            stop.set()
            return None

        def fail(self, job: UUID, token: UUID, code: str) -> bool:
            events.append('fail')
            return True

        def finish(self, job: UUID, token: UUID, data: dict, source: str) -> bool:
            events.append('finish')
            return True

    client = httpx.AsyncClient(transport=httpx.MockTransport(respond))
    monkeypatch.setattr(worker.httpx, 'AsyncClient', lambda **_kwargs: client)
    cfg = settings(check_dependencies=check_services, mode='fake', poll_seconds=0.001)

    async def run():
        await asyncio.wait_for(worker.worker_loop(cfg, Storage('unused'), stop), 3)

    asyncio.run(run())
    assert events == [
        ('probe', denial),
        ('probe', 204),
        'claim',
        ('context', denial),
        ('probe', denial),
        ('probe', 200),  # A successful generic readiness/SPA page is not this probe.
        ('probe', 204),
        'claim',
    ]
