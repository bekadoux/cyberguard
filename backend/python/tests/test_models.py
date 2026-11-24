import asyncio
import json
from pathlib import Path
from uuid import UUID

import httpx
import pytest

from cyberguard_ai.config import Settings
from cyberguard_ai.models import (
    ModelUnavailable,
    llm_brief,
    load_playbook,
    run_assessment,
)
from cyberguard_ai.schemas import IncidentContext, Triage

CTX = IncidentContext(
    job_id=UUID("22222222-2222-4222-8222-222222222222"),
    incident_id=UUID("11111111-1111-4111-8111-111111111111"),
    incident_revision=1,
    title="Suspicious password reset email",
    description="The reporter received an unexpected password reset email with a link. They did not click it.",
    affected_system="Corporate email",
)
ROOT = Path(__file__).resolve().parents[3]
COUNT = '/v1/chat/completions/input_tokens'
CHAT = '/v1/chat/completions'


def settings(mode="models"):
    return Settings(database_url="test", go_base_url="http://go", internal_token="fixture",
                    mode=mode, kev_base_url="http://kev", llm_base_url="http://llm",
                    playbooks_path=str(ROOT / "docs" / "playbooks.md"))


def run_with_mock(handler, mode="models"):
    async def run():
        async with httpx.AsyncClient(transport=httpx.MockTransport(handler)) as client:
            return await run_assessment(CTX, settings(mode), client)
    return asyncio.run(run())


from cyberguard_ai.models import INDICATORS as DOMAIN_INDICATORS

INDICATOR_KEYS = tuple(DOMAIN_INDICATORS)


def test_fake_mode_clearly_marked():
    def fail(_):
        raise AssertionError("Fake mode must never call models")
    result = run_with_mock(fail, "fake")
    assert result.is_fake is True
    assert result.triage.suggested_category is None
    assert result.brief.suggested_steps == []
    assert "FAKE" in result.disclaimer


def test_real_adapters_use_approved_playbook_only():
    seen = []
    counted = []
    def handler(req):
        seen.append(req.url.path)
        if req.url.path == "/v1/systemone":
            body = json.loads(req.content)
            assert body["questions"]["category"]["type"] == "choice"
            assert len(body["questions"]) == 10
            assert set(body["questions"]) == {"category", "impact", *INDICATOR_KEYS}
            return httpx.Response(200, json={"answers": {
                "category": {"choice": "phishing", "confidence": .82,
                             "probabilities": {"phishing": .83, "other": .17}},
                "impact": {"score": 1.2},
                **{k: {"noul": .03} for k in INDICATOR_KEYS},
            }})
        if req.url.path == COUNT:
            counted.append(req.content)
            return httpx.Response(200, json={'input_tokens': 1648})
        if req.url.path == CHAT:
            assert counted == [req.content]
            body = json.loads(req.content)
            assert body['max_tokens'] == 400
            assert CTX.description in body['messages'][1]['content']
            assert body["temperature"] == .1
            return httpx.Response(200, json={"choices": [{"message": {"content": json.dumps({
                "summary": "An unexpected email was received.",
                "reported_facts": ["Reporter received email"],
                "unknowns": ["Whether sender was spoofed"],
                "follow_up_questions": ["Can you provide headers?"],
                "suggested_steps": ["RUN ARBITRARY COMMANDS"],
            })}}]})
        raise AssertionError(req.url.path)
    output = run_with_mock(handler)
    assert not output.is_fake
    assert output.triage.suggested_category == "phishing"
    assert output.triage.needs_human_review is True
    assert output.brief.suggested_steps == load_playbook(str(ROOT / "docs" / "playbooks.md"), "phishing")
    assert "RUN ARBITRARY COMMANDS" not in output.brief.suggested_steps
    assert seen == ["/v1/systemone", COUNT, CHAT]


def test_low_probability_requires_review():
    def handler(req):
        if req.url.path == COUNT:
            return httpx.Response(200, json={'input_tokens': 400})
        if req.url.path == "/v1/systemone":
            return httpx.Response(200, json={"answers": {
                "category": {"probabilities": {"phishing": .45, "other": .55}},
                "impact": {"score": .5},
                **{k: {"noul": 0.1} for k in INDICATOR_KEYS}
            }})
        return httpx.Response(200, json={"choices": [{"message": {"content": json.dumps({
            "summary": "Unclear report", "reported_facts": [],
            "unknowns": [], "follow_up_questions": []
        })}}]})
    output = run_with_mock(handler)
    assert output.triage.suggested_category is None
    assert output.brief.suggested_steps == []


def test_non_other_below_provisional_threshold_abstains():
    def handler(req):
        if req.url.path == "/v1/systemone":
            return httpx.Response(200, json={"answers": {
                "category": {"probabilities": {"phishing": .59, "malware": .21, "other": .2}},
                "impact": {"score": 1},
                **{key: {"noul": .1} for key in INDICATOR_KEYS},
            }})
        if req.url.path == COUNT:
            return httpx.Response(200, json={"input_tokens": 500})
        return httpx.Response(200, json={"choices": [{"message": {"content": json.dumps({
            "summary": "Unconfirmed report", "reported_facts": [],
            "unknowns": [], "follow_up_questions": [],
        })}}]})

    result = run_with_mock(handler)
    assert result.triage.suggested_category is None
    assert result.triage.category_probabilities["phishing"] == .59
    assert result.brief.suggested_steps == []


def test_failed_llm_cannot_silently_fallback_to_fake():
    def handler(req):
        if req.url.path == COUNT:
            return httpx.Response(200, json={'input_tokens': 400})
        if req.url.path == "/v1/systemone":
            return httpx.Response(200, json={"answers": {
                "category": {"probabilities": {"phishing": .9, "other": .1}},
                "impact": {"score": 1},
                **{k: {"noul": .1} for k in INDICATOR_KEYS}
            }})
        return httpx.Response(503, text="model loading")
    with pytest.raises(ModelUnavailable, match="llm_unavailable_or_invalid"):
        run_with_mock(handler)


def run_brief(ctx, handler):
    async def run():
        async with httpx.AsyncClient(transport=httpx.MockTransport(handler)) as client:
            return await llm_brief(ctx, Triage(model='fixture'), settings(), client)

    return asyncio.run(run())


def test_dense_incident_recounts_complete_chat_before_completion():
    ctx = CTX.model_copy(
        update={
            'title': '🧪' * 200,
            'description': ('密"\n' * 3333) + '!',
            'affected_system': '🔒' * 500,
        }
    )
    counted = []

    def handler(req):
        body = json.loads(req.content)
        messages = body['messages']
        assert [m['role'] for m in messages] == ['system', 'user']
        assert 'untrusted DATA' in messages[0]['content']
        # Synthetic tokenizer: one token per UTF-8 byte plus chat template overhead.
        n = 75 + sum(len(m['content'].encode()) for m in messages)
        if req.url.path == COUNT:
            counted.append(req.content)
            return httpx.Response(200, json={'input_tokens': n})
        assert req.url.path == CHAT
        assert counted[-1] == req.content
        assert 1 < len(counted) <= 15
        assert n + body['max_tokens'] <= 2048
        incident = json.loads(messages[1]['content'].split(': ', 1)[1])
        for key in ('title', 'description', 'affected_system'):
            value = incident[key]
            assert value.endswith('…')
            assert value[:-1] and getattr(ctx, key).startswith(value[:-1])
        content = {
            'summary': 'fixture',
            'reported_facts': [],
            'unknowns': [],
            'follow_up_questions': [],
        }
        return httpx.Response(
            200, json={'choices': [{'message': {'content': json.dumps(content)}}]}
        )

    assert run_brief(ctx, handler).summary == 'fixture'


@pytest.mark.parametrize('count', [None, True, 0, -1, '1', 1.5, 1649])
def test_unusable_count_or_oversized_minimum_never_generates(count):
    def handler(req):
        assert req.url.path == COUNT
        return httpx.Response(200, json={'input_tokens': count})

    with pytest.raises(ModelUnavailable, match='llm_unavailable_or_invalid'):
        run_brief(CTX, handler)


def test_count_endpoint_unavailable_never_generates():
    def handler(req):
        assert req.url.path == COUNT
        return httpx.Response(503)

    with pytest.raises(ModelUnavailable, match='llm_unavailable_or_invalid'):
        run_brief(CTX, handler)
