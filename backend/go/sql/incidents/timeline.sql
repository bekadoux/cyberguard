-- name: ListReporterTimeline :one
-- Authorization, page read and existence check share one statement snapshot.
-- A nonexistent or inaccessible incident produces zero rows; an accessible
-- incident with no more events produces [].
SELECT COALESCE((
    SELECT jsonb_agg(
        jsonb_build_object('status', page.status, 'occurred_at', page.occurred_at, 'key', page.event_key)
        ORDER BY page.occurred_at DESC, page.event_key COLLATE "C" DESC
    )
    FROM (
        SELECT events.status, events.occurred_at, events.event_key
        FROM (
            SELECT 'submitted'::text AS status, i.created_at AS occurred_at,
                   repeat('0', 64) AS event_key
            UNION ALL
            SELECT CASE WHEN a.action = 'claimed' THEN 'triage' ELSE a.details->>'to' END,
                   a.created_at, encode(sha256(uuid_send(a.id)), 'hex')
            FROM incident_actions AS a
            WHERE a.incident_id = i.id
              AND (
                  (a.action = 'claimed' AND a.details->>'status' = 'triage')
                  OR (
                      a.action = 'status_changed'
                      AND a.details->>'from' IN ('submitted', 'triage', 'investigating',
                          'awaiting_reporter', 'resolved', 'closed', 'false_positive')
                      AND a.details->>'to' IN ('triage', 'investigating',
                          'awaiting_reporter', 'resolved', 'closed', 'false_positive')
                      AND a.details->>'from' <> a.details->>'to'
                  )
              )
        ) AS events
        WHERE sqlc.narg(before_time)::timestamptz IS NULL
           OR (events.occurred_at, events.event_key COLLATE "C")
                < (sqlc.narg(before_time)::timestamptz, sqlc.arg(before_key)::text COLLATE "C")
        ORDER BY events.occurred_at DESC, events.event_key COLLATE "C" DESC
        LIMIT sqlc.arg(page_limit)
    ) AS page
), '[]'::jsonb)::jsonb AS events
FROM incidents AS i
WHERE i.id = sqlc.arg(incident_id) AND i.reporter_id = sqlc.arg(reporter_id) AND i.archived_at IS NULL;
