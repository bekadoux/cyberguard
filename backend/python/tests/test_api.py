from datetime import datetime, timezone
from uuid import UUID
from fastapi.testclient import TestClient
from cyberguard_ai.api import create_app
from cyberguard_ai.config import Settings

ID = UUID("11111111-1111-4111-8111-111111111111")
JOB = UUID("22222222-2222-4222-8222-222222222222")


class FakeStore:
    def __init__(self):
        self.visited = []
    def status(self, ident):
        self.visited.append(ident)
        return {
            "id": JOB, "incident_id": ID, "kind": "initial_assessment",
            "status": "completed", "incident_revision": 1, "attempt_count": 1,
            "created_at": "2026-11-09T12:00:00Z", "updated_at": "2026-11-09T12:00:01Z",
            "finished_at": "2026-11-09T12:00:01Z", "error_code": None,
        }
    def result(self, ident):
        self.visited.append(ident)
        return {"assessment": {"is_fake": True}, "source_kind": "fake",
                "created_at": datetime(2026, 11, 9, tzinfo=timezone.utc)}
    def ping(self):
        pass


def test_read_needs_go_authorization(monkeypatch):
    async def denies(*_args):
        return 403
    monkeypatch.setattr("cyberguard_ai.api.authorize_analyst", denies)
    storage = FakeStore()
    app = create_app(Settings(database_url="unused", go_base_url="http://go", internal_token="test"), storage)
    with TestClient(app) as client:
        resp = client.get(f"/incidents/{ID}/assessment")
        assert resp.status_code == 403
        assert not storage.visited


def test_status_and_json_serialization(monkeypatch):
    async def permits(*_args):
        return 200
    monkeypatch.setattr("cyberguard_ai.api.authorize_analyst", permits)
    storage = FakeStore()
    app = create_app(Settings(database_url="unused", go_base_url="http://go", internal_token="test"), storage)
    with TestClient(app) as client:
        resp = client.get(f"/incidents/{ID}/assessment")
        assert resp.status_code == 200
        assert resp.headers["cache-control"] == "no-store"
        assert resp.json()["job"]["id"] == str(JOB)
        assert resp.json()["result"]["source_kind"] == "fake"
        assert len(storage.visited) == 2
        assert client.get("/readyz").json() == {"status": "ready"}
