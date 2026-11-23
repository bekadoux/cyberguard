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
- [ ] Redesign the staff incident assessment and human review.
  - Keep Kev's recommendation visible even when the chart is collapsed. If Kev
    abstains, show “No recommendation,” the highest-scoring category and its
    score when available, and whether `other` won or the score fell below the
    suggestion threshold. Do not present that top result as a recommendation or
    enable Accept for it.
  - Show category scores in a collapsible horizontal bar chart, ranked highest
    first and labelled with category names and percentages. Default it to closed
    when Kev recommends a category and open when Kev abstains. Preserve staff's
    manual toggle on refresh of the same assessment. Keep indicators separate.
  - For Accept, show the category as text instead of a picker. For Correct, show
    the picker; when there is no recommendation, label that action “Choose a
    category.” Reject still leaves the confirmed category unchanged.
  - Group the LLM summary, reported facts, unknowns, and follow-up questions in
    a collapsible “AI draft brief” that starts expanded. Keep approved playbook
    steps separate from the generated brief.
- [ ] Add an “AI assessment” status column to the staff incident queue, separate
  from incident status. Show Queued, In progress, Complete, and Failed; Complete
  means the job finished, not that it produced a recommendation or was reviewed.
  Add the job status, not the assessment itself, to the staff list response.
  Omit it from employee responses and update it with the existing manual list
  refresh. Do not poll or fetch an assessment per row.
- [ ] Redesign the sign-in page and add a sign-up page based on the team's
  concept images.

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
