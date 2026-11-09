"""Restricted worker database operations. Claim, renew and completion are fenced SQL functions."""
from dataclasses import dataclass
from uuid import UUID


@dataclass(frozen=True)
class Claim:
    job_id: UUID
    incident_id: UUID
    incident_revision: int
    claim_token: UUID


class Storage:
    def __init__(self, url: str):
        self.url = url

    def claim(self, attempts: int, lease: int) -> Claim | None:
        import psycopg
        from psycopg.rows import dict_row
        with psycopg.connect(self.url, row_factory=dict_row, connect_timeout=5) as conn:
            row = conn.execute("SELECT * FROM ai_claim_job(%s, %s)", (attempts, lease)).fetchone()
            return Claim(**row) if row else None

    def renew(self, job: UUID, token: UUID, lease: int) -> bool:
        import psycopg
        with psycopg.connect(self.url, connect_timeout=5) as conn:
            return bool(conn.execute("SELECT ai_renew_claim(%s, %s, %s)", (job, token, lease)).fetchone()[0])

    def finish(self, job: UUID, token: UUID, data: dict, source: str) -> bool:
        import psycopg
        from psycopg.types.json import Jsonb
        with psycopg.connect(self.url, connect_timeout=5) as conn:
            return bool(conn.execute("SELECT ai_finish_job(%s, %s, %s, %s)",
                                     (job, token, Jsonb(data), source)).fetchone()[0])

    def fail(self, job: UUID, token: UUID, code: str) -> bool:
        import psycopg
        with psycopg.connect(self.url, connect_timeout=5) as conn:
            return bool(conn.execute("SELECT ai_fail_job(%s, %s, %s)", (job, token, code)).fetchone()[0])

    def status(self, incident: UUID) -> dict | None:
        import psycopg
        from psycopg.rows import dict_row
        with psycopg.connect(self.url, row_factory=dict_row, connect_timeout=5) as conn:
            row = conn.execute("""
                SELECT id, incident_id, kind, status, incident_revision,
                       attempt_count, created_at, updated_at, finished_at, error_code
                FROM assessment_jobs WHERE incident_id=%s AND kind='initial_assessment'
            """, (incident,)).fetchone()
            if row:
                for key in ("created_at", "updated_at", "finished_at"):
                    row[key] = row[key].isoformat() if row[key] else None
            return row

    def result(self, incident: UUID) -> dict | None:
        import psycopg
        from psycopg.rows import dict_row
        with psycopg.connect(self.url, row_factory=dict_row, connect_timeout=5) as conn:
            return conn.execute("""
                SELECT r.assessment, r.source_kind, r.created_at FROM assessment_results r
                JOIN assessment_jobs j ON j.id=r.job_id
                WHERE j.incident_id=%s AND j.kind='initial_assessment' AND j.status='completed'
            """, (incident,)).fetchone()

    def ping(self) -> None:
        import psycopg
        with psycopg.connect(self.url, connect_timeout=5) as conn:
            conn.execute("SELECT 1")
