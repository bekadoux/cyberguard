import asyncio
from uuid import UUID

import httpx
import pytest
from cyberguard_ai.config import Settings
from cyberguard_ai.go_client import ContextUnavailable, authorize_analyst, job_context

ID = UUID('11111111-1111-4111-8111-111111111111')
JOB = UUID('22222222-2222-4222-8222-222222222222')
ACTOR = UUID('33333333-3333-4333-8333-333333333333')
cfg = Settings(database_url='test', go_base_url='http://go', internal_token='secret')


def request_as(role, detail_status=200, reporter_id=ID):
    def handler(req):
        assert req.headers.get('cookie') == 'session=demo'
        if req.url.path == '/auth/me':
            return httpx.Response(200, json={'user': {'id': str(ACTOR), 'role': role}})
        if req.url.path == f'/incidents/{ID}':
            return httpx.Response(
                detail_status,
                json={'incident': {'id': str(ID), 'reporter_id': str(reporter_id)}},
            )
        raise AssertionError(req.url.path)

    async def run():
        async with httpx.AsyncClient(transport=httpx.MockTransport(handler)) as http:
            return await authorize_analyst(ID, 'session=demo', cfg, http)

    return asyncio.run(run())


def test_role_and_incident_access():
    assert request_as('employee') == 403
    assert request_as('analyst', 404) == 404
    assert request_as('analyst') == 200
    assert request_as('admin') == 200


@pytest.mark.parametrize('role', ['analyst', 'admin'])
def test_staff_cannot_read_ai_for_their_own_report(role):
    assert request_as(role, reporter_id=ACTOR) == 403


@pytest.mark.parametrize('role', ['analyst', 'admin'])
def test_expired_session_during_incident_lookup_remains_unauthorized(role):
    assert request_as(role, detail_status=401) == 401


@pytest.mark.parametrize(
    'me, detail',
    [
        (
            {'user': {'role': 'analyst'}},
            {'incident': {'id': str(ID), 'reporter_id': str(ID)}},
        ),
        ({'user': {'id': 'invalid', 'role': 'admin'}}, {}),
        ({'user': {'id': str(ACTOR), 'role': 'analyst'}}, {'incident': {'id': str(ID)}}),
        ({'user': {'id': str(ACTOR), 'role': 'analyst'}}, {'incident': None}),
        (
            {'user': {'id': str(ACTOR), 'role': 'analyst'}},
            {'incident': {'id': str(JOB), 'reporter_id': str(ID)}},
        ),
        ([], {}),
    ],
)
def test_malformed_or_mismatched_go_data_fails_closed(me, detail):
    async def run():
        async with httpx.AsyncClient(
            transport=httpx.MockTransport(
                lambda req: httpx.Response(
                    200, json=me if req.url.path == '/auth/me' else detail
                )
            )
        ) as client:
            return await authorize_analyst(ID, 'synthetic-session', cfg, client)

    assert asyncio.run(run()) == 503


def test_context_requires_service_token_and_matches_shape():
    def handler(req):
        assert req.headers['Authorization'] == 'Bearer secret'
        return httpx.Response(
            200,
            json={
                'job_id': str(JOB),
                'incident_id': str(ID),
                'incident_revision': 1,
                'title': 'Test',
                'description': 'Reported test',
                'affected_system': None,
            },
        )

    async def run():
        async with httpx.AsyncClient(transport=httpx.MockTransport(handler)) as client:
            return await job_context(JOB, cfg, client)

    assert asyncio.run(run()).job_id == JOB


def test_stale_context_rejected():
    async def run():
        async with httpx.AsyncClient(
            transport=httpx.MockTransport(lambda _: httpx.Response(409))
        ) as client:
            return await job_context(JOB, cfg, client)

    try:
        asyncio.run(run())
    except ContextUnavailable as exc:
        assert exc.code == 'stale_context'
    else:
        raise AssertionError('stale context must fail')
