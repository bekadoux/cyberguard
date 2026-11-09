from dataclasses import dataclass
import os
from pathlib import Path


def secret(name: str, default: str = "") -> str:
    direct = os.environ.get(name)
    secret_file = os.environ.get(f"{name}_FILE")
    if direct is not None and secret_file is not None:
        raise ValueError(f"{name}: configure either direct or _FILE")
    if secret_file:
        data = Path(secret_file).read_text(encoding="utf-8")
        return data.removesuffix("\n").removesuffix("\r")
    return direct if direct is not None else default


@dataclass(frozen=True)
class Settings:
    database_url: str
    go_base_url: str
    internal_token: str
    kev_base_url: str = "http://127.0.0.1:8009"
    kev_model: str = "kev-latest"
    llm_base_url: str = "http://127.0.0.1:8081"
    llm_model: str = "qwen2.5-1.5b-instruct"
    mode: str = "models"  # 'fake' is ONLY a labelled integration test fixture
    playbooks_path: str = "docs/playbooks.md"
    poll_seconds: float = 3.0
    lease_seconds: int = 900
    max_attempts: int = 3
    request_timeout: float = 180.0

    @classmethod
    def from_env(cls) -> "Settings":
        mode = os.environ.get("AI_MODE", "models")
        if mode not in ("models", "fake"):
            raise ValueError("AI_MODE must be models or fake")
        result = cls(
            database_url=secret("AI_DATABASE_URL"),
            go_base_url=os.environ.get("AI_GO_URL", "http://127.0.0.1:8080").rstrip("/"),
            internal_token=secret("AI_INTERNAL_TOKEN"),
            kev_base_url=os.environ.get("AI_KEV_URL", "http://127.0.0.1:8009").rstrip("/"),
            llm_base_url=os.environ.get("AI_LLM_URL", "http://127.0.0.1:8081").rstrip("/"),
            kev_model=os.environ.get("AI_KEV_MODEL", "kev-latest"),
            llm_model=os.environ.get("AI_LLM_MODEL", "qwen2.5-1.5b-instruct"),
            mode=mode,
            playbooks_path=os.environ.get("AI_PLAYBOOKS_PATH", "docs/playbooks.md"),
            poll_seconds=float(os.environ.get("AI_POLL_SECONDS", "3")),
            lease_seconds=int(os.environ.get("AI_LEASE_SECONDS", "900")),
            max_attempts=int(os.environ.get("AI_MAX_ATTEMPTS", "3")),
            request_timeout=float(os.environ.get("AI_REQUEST_TIMEOUT", "180")),
        )
        if not result.database_url or not result.internal_token:
            raise ValueError("AI_DATABASE_URL and AI_INTERNAL_TOKEN required")
        if not (30 <= result.lease_seconds <= 3600 and 1 <= result.max_attempts <= 10):
            raise ValueError("Invalid lease or attempts")
        if result.poll_seconds <= 0 or result.request_timeout <= 0:
            raise ValueError("Invalid timeout")
        return result
