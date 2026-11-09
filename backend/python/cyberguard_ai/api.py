"""Read-only AI results API. Browser authorization delegated to Go on every request."""
import httpx
from fastapi import FastAPI, HTTPException, Request
from fastapi.responses import JSONResponse
from fastapi.encoders import jsonable_encoder
from .config import Settings
from .db import Storage
from .go_client import authorize_analyst
from uuid import UUID


def create_app(cfg: Settings | None = None, storage: Storage | None = None) -> FastAPI:
    app = FastAPI(title="CyberGuard AI", version="0.1.0", docs_url=None, redoc_url=None,
                  openapi_url=None)
    app.state.config = cfg
    app.state.storage = storage

    @app.get("/healthz")
    async def healthz():
        return {"status": "ok"}

    @app.get("/readyz")
    async def readyz():
        db = app.state.storage or Storage((app.state.config or Settings.from_env()).database_url)
        try:
            import asyncio
            await asyncio.to_thread(db.ping)
        except Exception:
            raise HTTPException(status_code=503, detail="AI storage unavailable")
        return {"status": "ready"}

    @app.get("/incidents/{incident_id}/assessment")
    async def assessment(incident_id: UUID, request: Request):
        cfg = app.state.config or Settings.from_env()
        async with httpx.AsyncClient(trust_env=False) as client:
            code = await authorize_analyst(incident_id, request.headers.get("cookie", ""), cfg, client)
        if code != 200:
            raise HTTPException(status_code=code, detail="Access denied or unavailable")
        import asyncio
        db = app.state.storage or Storage(cfg.database_url)
        try:
            job = await asyncio.to_thread(db.status, incident_id)
            result = await asyncio.to_thread(db.result, incident_id) if job and job["status"] == "completed" else None
        except Exception:
            raise HTTPException(status_code=503, detail="AI storage unavailable")
        if not job:
            raise HTTPException(status_code=404, detail="Assessment job not found")
        return JSONResponse(jsonable_encoder({
            "job": job,
            "result": {"assessment": result["assessment"], "source_kind": result["source_kind"],
                       "created_at": result["created_at"].isoformat()} if result else None,
        }), headers={"Cache-Control": "no-store"})

    return app


app = create_app()
