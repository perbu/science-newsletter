-- name: CreateAuthToken :exec
INSERT INTO auth_tokens (token, email, expires_at)
VALUES (?, ?, ?);

-- name: GetAuthTokenByToken :one
SELECT * FROM auth_tokens
WHERE token = ? AND used_at IS NULL AND expires_at > CURRENT_TIMESTAMP;

-- name: MarkAuthTokenUsed :exec
UPDATE auth_tokens SET used_at = CURRENT_TIMESTAMP WHERE token = ?;

-- name: CreateSession :exec
INSERT INTO sessions (token, email, expires_at)
VALUES (?, ?, ?);

-- name: GetSessionByToken :one
SELECT * FROM sessions
WHERE token = ? AND expires_at > CURRENT_TIMESTAMP;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE token = ?;

-- name: DeleteExpiredAuthTokens :exec
DELETE FROM auth_tokens WHERE expires_at <= CURRENT_TIMESTAMP;

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE expires_at <= CURRENT_TIMESTAMP;
