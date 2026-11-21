# Temporary backlog

This is the single list of unfinished work and acceptance decisions from the
documentation revision. The [architecture overview](../README.md#architecture)
links to the component docs, which describe current behavior.
Deferred ideas are not release commitments.

## Agreed product work

- [ ] Add a reporter-safe status timeline. Expose only events relevant to the
  reporter; do not expose professional metadata, internal notes, or AI output.
- [ ] Align AI triage with the domain's eight-indicator rubric. Update the model/API/UI
  together, determine thresholds after evaluation, and highlight supported signals
  on the staff incident view. Signals must never automatically change severity.
  The domain rubric is in [English](domain-specification.md#indicators-yesno-questions)
  and [Slovak](domain-specification.sk.md#indikátory-otázky-ánonie).
- [ ] Add an explicit, repeatable CLI seed for a small curated set of ordinary demo
  incidents. Keep it scoped to intended demo data; do not add an admin CSV importer.

## Validation and release work

- [ ] Run the full demo on the target hosts with real Kev/Qwen: submit a synthetic
  report, close the submitting browser, inspect progress as staff, verify the
  original snapshot and access rules, ask the reporter a question, record a human
  decision, update severity separately, and complete resolution/confirmation.
  Verify core workflows also work during AI outages.
- [ ] Evaluate English and Slovak reports against reviewed labels. Establish category
  and indicator metrics, calibration/uncertainty thresholds, and a rubric for
  brief usefulness, factual errors, and unsupported claims. Review model licenses,
  provenance, playbook content, and model-server logging before handling real data.
- [ ] Measure model startup and warm latency, peak RAM/CPU, disk growth, and core API
  responsiveness on the target VMs. Confirm sizing, quotas, and cost.
- [ ] Verify actual Azure bootstrap, private-source allow/deny rules, public
  certificate issuance/renewal, deployed browser cookies and proxy policy, and
  cold-boot persistence/recovery. Rehearse off-host backup restoration and scoped
  credential/certificate rotation. Existing disposable fixture runs are not this evidence.
- [ ] Review the deployed AI schema/service/grant contracts with maintainers and
  exercise competing claims, lease expiry/recovery, and dependency failure against
  real PostgreSQL. Preserve job fencing and no-business-write guarantees.
- [ ] Decide and document safe handling for pending legacy jobs if a real deployment
  has them. Retrying terminal failures, regenerating assessments, or backfilling
  historical context require separate reviewed policies; none is implied by that decision.
- [ ] Decide on CI coverage for the existing Make checks. Recheck the Firefox
  accessibility scanner workaround after browser/tool upgrades, and remove the
  temporary frontend `minimatch` override when the lint dependencies resolve to
  a patched release on their own.

## Deferred or conditional

- Before a production or multi-replica rollout, decide on operating controls such
  as ingress/admin abuse limits, scheduled/off-host backups, pagination for
  comments/history/notifications, and dedicated scheduling for auto-close.
  Cross-tab auth mutation ordering and stronger cross-database coordination are
  separate design decisions if needed.
- Original optional scope: attachments, asset inventory beyond free text,
  investigation tasks, PDF reports, complex escalations, outbound integrations,
  general-purpose import, advanced analytics, a general-purpose chatbot/agent, and
  BPMN. If pursuing the coursework BPMN bonus, confirm the recorded target of at
  least 60 activities, two pools, at least three lanes in one pool, a subprocess,
  and gateways with the course team.
