-- name: GetNode :one
SELECT * FROM nodes
WHERE mac_addr = ? LIMIT 1;

-- name: ListNodes :many
SELECT * FROM nodes
ORDER BY hostname;

-- name: CreateNode :one
INSERT INTO nodes (
  mac_addr, hostname, ip_addr, uci_dhcp_start, uci_dhcp_limit, created_at, updated_at
) VALUES (
  ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
)
ON CONFLICT(mac_addr) DO UPDATE SET
  hostname = excluded.hostname,
  ip_addr = excluded.ip_addr,
  uci_dhcp_start = excluded.uci_dhcp_start,
  uci_dhcp_limit = excluded.uci_dhcp_limit,
  updated_at = CURRENT_TIMESTAMP
RETURNING *;

-- name: UpdateNode :exec
UPDATE nodes
set hostname = ?,
ip_addr = ?,
uci_dhcp_start = ?,
uci_dhcp_limit = ?,
updated_at = CURRENT_TIMESTAMP
WHERE mac_addr = ?;

-- name: DeleteNode :exec
DELETE FROM nodes
WHERE mac_addr = ?;
