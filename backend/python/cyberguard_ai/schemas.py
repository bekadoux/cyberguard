from typing import Literal
from uuid import UUID
from pydantic import BaseModel, Field, ConfigDict

Category = Literal[
    "phishing", "malware", "account_compromise", "data_exposure", "lost_device",
    "policy_violation", "service_disruption", "other"
]


class IncidentContext(BaseModel):
    model_config = ConfigDict(extra="forbid")
    job_id: UUID
    incident_id: UUID
    incident_revision: int = Field(ge=1)
    title: str = Field(min_length=1, max_length=200)
    description: str = Field(min_length=1, max_length=10000)
    affected_system: str | None = Field(default=None, max_length=500)


class Triage(BaseModel):
    suggested_category: Category | None = None
    category_probabilities: dict[str, float] = Field(default_factory=dict)
    potential_impact: float | None = Field(default=None, ge=0, le=3)
    indicator_probabilities: dict[str, float] = Field(default_factory=dict)
    needs_human_review: bool = True
    model: str
    # Underlying choice confidence is NOT treated as a calibrated risk score.
    raw_model_confidence: float | None = None


class Brief(BaseModel):
    summary: str = Field(min_length=1, max_length=900)
    reported_facts: list[str] = Field(max_length=5)
    unknowns: list[str] = Field(max_length=5)
    follow_up_questions: list[str] = Field(max_length=5)
    suggested_steps: list[str] = Field(max_length=5)


class Assessment(BaseModel):
    schema_version: Literal["1.0"] = "1.0"
    is_fake: bool = False
    triage: Triage
    brief: Brief
    disclaimer: str = "AI-generated draft for analyst review; not verified evidence or an automated response decision."


class Job(BaseModel):
    id: UUID
    incident_id: UUID
    kind: str
    status: Literal["queued", "running", "completed", "failed"]
    incident_revision: int
    attempt_count: int
    created_at: str
    updated_at: str
    finished_at: str | None
    error_code: str | None
