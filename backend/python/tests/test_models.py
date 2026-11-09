import asyncio
import json
from pathlib import Path
from uuid import UUID
import httpx
import pytest
from cyberguard_ai.config import Settings
from cyberguard_ai.schemas import IncidentContext
from cyberguard_ai.models import run_assessment, ModelUnavailable, load_playbook

CTX = IncidentContext(
    job_id=UUID("22222222-2222-4222-8222-222222222222"),
    incident_id=UUID("11111111-1111-4111-8111-111111111111"),
    incident_revision=1,
    title="Suspicious password reset email",
    description="The reporter received an unexpected password reset email with a link. They did not click it.",
    affected_system="Corporate email",
)
ROOT = Path(__file__).resolve().parents[3]


def settings(mode="models"):
    return Settings(database_url="test", go_base_url="http://go", internal_token="fixture",
                    mode=mode, kev_base_url="http://kev", llm_base_url="http://llm",
                    playbooks_path=str(ROOT / "docs" / "playbooks.md"))


def run_with_mock(handler, mode="models"):
    async def run():
        async with httpx.AsyncClient(transport=httpx.MockTransport(handler)) as client:
            return await run_assessment(CTX, settings(mode), client)
    return asyncio.run(run())


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
    def handler(req):
        seen.append(req.url.path)
        if req.url.path == "/v1/systemone":
            body = json.loads(req.content)
            assert body["questions"]["category"]["type"] == "choice"
            assert len(body["questions"]) == 6
            return httpx.Response(200, json={"answers": {
                "category": {"choice": "phishing", "confidence": .82,
                             "probabilities": {"phishing": .83, "other": .17}},
                "impact": {"score": 1.2},
                "clicked_suspicious_link": {"noul": .01},
                "credentials_entered": {"noul": .03},
                "files_encrypted": {"noul": .02},
                "unauthorized_access": {"noul": .01},
            }})
        if req.url.path == "/v1/chat/completions":
            body = json.loads(req.content)
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
    assert seen == ["/v1/systemone", "/v1/chat/completions"]


def test_low_probability_requires_review():
    def handler(req):
        if req.url.path == "/v1/systemone":
            return httpx.Response(200, json={"answers": {
                "category": {"probabilities": {"phishing": .45, "other": .55}},
                "impact": {"score": .5},
                **{k: {"noul": 0.1} for k in (
                    "clicked_suspicious_link", "credentials_entered",
                    "files_encrypted", "unauthorized_access")}
            }})
        return httpx.Response(200, json={"choices": [{"message": {"content": json.dumps({
            "summary": "Unclear report", "reported_facts": [],
            "unknowns": [], "follow_up_questions": []
        })}}]})
    output = run_with_mock(handler)
    assert output.triage.suggested_category is None
    assert output.brief.suggested_steps == []


def test_failed_llm_cannot_silently_fallback_to_fake():
    def handler(req):
        if req.url.path == "/v1/systemone":
            return httpx.Response(200, json={"answers": {
                "category": {"probabilities": {"phishing": .9, "other": .1}},
                "impact": {"score": 1},
                **{k: {"noul": .1} for k in (
                    "clicked_suspicious_link", "credentials_entered", "files_encrypted", "unauthorized_access")}
            }})
        return httpx.Response(503, text="model loading")
    with pytest.raises(ModelUnavailable, match="llm_unavailable_or_invalid"):
        run_with_mock(handler)
