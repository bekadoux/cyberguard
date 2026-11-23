-- +goose Up
-- Wait for any in-flight 00008 writer, then prevent an old API from creating
-- a first batch without recording scenario IDs after this migration.
LOCK TABLE demo_incident_batch IN ACCESS EXCLUSIVE MODE;
REVOKE INSERT ON demo_incident_batch FROM go_incidents;

CREATE TABLE demo_incident_scenarios (
    scenario_id TEXT PRIMARY KEY CHECK (scenario_id <> ''),
    incident_id UUID NOT NULL UNIQUE REFERENCES incidents(id)
);

-- Fixed 00008 catalog titles. Only audited demo submissions count, including
-- archived incidents. Titles are immutable; this mapping is not a live catalog.
WITH legacy(scenario_id, title) AS (VALUES
    ('password-verification', '[Demo] Unexpected password verification email'),
    ('email-link-login', '[Demo] Entered login after following an email link'),
    ('renamed-files-ransom', '[Demo] Files renamed and ransom note appeared'),
    ('mailbox-sent-messages', '[Demo] Messages sent from my mailbox'),
    ('wrong-spreadsheet-recipient', '[Demo] Spreadsheet went to the wrong recipient'),
    ('laptop-on-train', '[Demo] Laptop left on the train'),
    ('unapproved-remote-support', '[Demo] Unapproved remote support software'),
    ('staff-portal-outage', '[Demo] Staff portal unavailable'),
    ('slow-laptop', '[Demo] Laptop running unusually slowly'),
    ('supplier-payment-change', '[Demo] Supplier changed payment details'),
    ('gift-card-request', '[Demo] Suspicious gift card request'),
    ('browser-homepage-change', '[Demo] Browser homepage changed'),
    ('sk-courier-sms', '[Demo] Nezvyčajná SMS od kuriéra'),
    ('sk-lost-phone', '[Demo] Stratený služobný telefón'),
    ('sk-unrecognized-login', '[Demo] Neznáme prihlásenie do účtu')
)
INSERT INTO demo_incident_scenarios (scenario_id, incident_id)
SELECT legacy.scenario_id, i.id
FROM incidents i JOIN legacy ON i.title = legacy.title
WHERE EXISTS (
    SELECT 1 FROM incident_actions a WHERE a.incident_id = i.id AND a.action = 'demo_seeded'
);

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM incident_actions a
        WHERE a.action = 'demo_seeded' AND NOT EXISTS (
            SELECT 1 FROM demo_incident_scenarios s WHERE s.incident_id = a.incident_id
        )
    ) THEN
        RAISE EXCEPTION 'unrecognized legacy demo scenario; review before migrating';
    END IF;
END;
$$;
-- +goose StatementEnd
GRANT SELECT, INSERT ON demo_incident_scenarios TO go_incidents;

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    RAISE EXCEPTION 'demo scenario history is forward-only; review rollback explicitly';
END;
$$;
-- +goose StatementEnd
