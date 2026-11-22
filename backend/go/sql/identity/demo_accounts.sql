-- name: MarkDemoAccount :exec
INSERT INTO demo_accounts (user_id) VALUES ($1);

-- name: LockDemoReporters :many
SELECT u.id
FROM users u
JOIN demo_accounts d ON d.user_id = u.id
WHERE NOT u.is_blocked
  AND ((u.username IN ('employee_a', 'employee_b') AND u.role = 'employee')
       OR (u.username = 'analyst' AND u.role = 'analyst'))
ORDER BY u.username
FOR SHARE OF u;
