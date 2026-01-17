-- name: GetMeshNode :one
SELECT * FROM mesh_nodes
WHERE mac_addr = ? LIMIT 1;

-- name: GetMeshNodeByHostname :one
SELECT * FROM mesh_nodes
WHERE hostname = ? LIMIT 1;

-- name: ListMeshNodes :many
SELECT * FROM mesh_nodes
ORDER BY hostname;

-- name: CreateMeshNode :one
INSERT INTO mesh_nodes (
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

-- name: UpdateMeshNode :exec
UPDATE mesh_nodes
set hostname = ?,
ip_addr = ?,
uci_dhcp_start = ?,
uci_dhcp_limit = ?,
updated_at = CURRENT_TIMESTAMP
WHERE mac_addr = ?;

-- name: DeleteMeshNode :exec
DELETE FROM mesh_nodes
WHERE mac_addr = ?;

-- name: DeleteAllMeshNodes :exec
DELETE FROM mesh_nodes;
