package webhooks

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Scopes define what an API key is allowed to do.
// Checked in RequireScope middleware.
const (
	ScopeLibraryRead    = "library:read"
	ScopeRequestsRead   = "requests:read"
	ScopeRequestsWrite  = "requests:write"
	ScopeTransfersRead  = "transfers:read"
	ScopeTransfersWrite = "transfers:write" // pause/resume
	ScopeWebhooksRead   = "webhooks:read"
	ScopeWebhooksWrite  = "webhooks:write"
)

// APIKey is a machine-to-machine access key (secret shown once on creation).
type APIKey struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Scopes    []string  `json:"scopes"`
	CreatedBy string    `json:"created_by"`
	LastUsed  *time.Time `json:"last_used,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

// CreateAPIKey generates a new API key and returns both the record and the
// raw key string (prefix + secret). The raw key is only returned here —
// only its SHA-256 hash is stored.
//
// Raw key format: "mm_<32 random hex bytes>"
// This makes it easy to identify MediaMesh keys in external systems.
func CreateAPIKey(ctx context.Context, db *sql.DB, name, createdBy string, scopes []string) (key *APIKey, rawKey string, err error) {
	secretBytes := make([]byte, 32)
	if _, err = rand.Read(secretBytes); err != nil {
		return nil, "", fmt.Errorf("apikeys.Create: generate: %w", err)
	}
	rawKey = "mm_" + hex.EncodeToString(secretBytes)
	hash := sha256Key(rawKey)
	scopesJSON, _ := json.Marshal(scopes)
	id := uuid.New().String()

	_, err = db.ExecContext(ctx,
		`INSERT INTO api_keys (id, name, key_hash, created_by, scopes) VALUES (?, ?, ?, ?, ?)`,
		id, name, hash, createdBy, string(scopesJSON),
	)
	if err != nil {
		return nil, "", fmt.Errorf("apikeys.Create: insert: %w", err)
	}
	return &APIKey{
		ID:        id,
		Name:      name,
		Scopes:    scopes,
		CreatedBy: createdBy,
		CreatedAt: time.Now().UTC(),
	}, rawKey, nil
}

// ValidateAPIKey looks up a raw key, marks last_used, and returns the key record.
// Returns an error if the key doesn't exist or is revoked.
func ValidateAPIKey(ctx context.Context, db *sql.DB, rawKey string) (*APIKey, error) {
	hash := sha256Key(rawKey)
	row := db.QueryRowContext(ctx,
		`SELECT id, name, scopes, created_by, last_used, created_at, revoked_at
         FROM api_keys WHERE key_hash = ?`, hash,
	)
	var k APIKey
	var scopesJSON string
	var lastUsed, revokedAt sql.NullTime
	if err := row.Scan(&k.ID, &k.Name, &scopesJSON, &k.CreatedBy, &lastUsed, &k.CreatedAt, &revokedAt); err != nil {
		return nil, fmt.Errorf("apikeys.Validate: %w", err)
	}
	if revokedAt.Valid {
		return nil, fmt.Errorf("apikeys.Validate: key revoked")
	}
	_ = json.Unmarshal([]byte(scopesJSON), &k.Scopes)
	if lastUsed.Valid {
		k.LastUsed = &lastUsed.Time
	}

	_, _ = db.ExecContext(ctx, `UPDATE api_keys SET last_used = CURRENT_TIMESTAMP WHERE id = ?`, k.ID)
	return &k, nil
}

// ListAPIKeys returns all keys for display (hashes and secrets never returned).
func ListAPIKeys(ctx context.Context, db *sql.DB) ([]*APIKey, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, name, scopes, created_by, last_used, created_at, revoked_at
         FROM api_keys ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("apikeys.List: %w", err)
	}
	defer rows.Close()

	var out []*APIKey
	for rows.Next() {
		var k APIKey
		var scopesJSON string
		var lastUsed, revokedAt sql.NullTime
		if err := rows.Scan(&k.ID, &k.Name, &scopesJSON, &k.CreatedBy, &lastUsed, &k.CreatedAt, &revokedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(scopesJSON), &k.Scopes)
		if lastUsed.Valid {
			k.LastUsed = &lastUsed.Time
		}
		if revokedAt.Valid {
			k.RevokedAt = &revokedAt.Time
		}
		out = append(out, &k)
	}
	return out, nil
}

// RevokeAPIKey marks a key as revoked.
func RevokeAPIKey(ctx context.Context, db *sql.DB, id string) error {
	_, err := db.ExecContext(ctx,
		`UPDATE api_keys SET revoked_at = CURRENT_TIMESTAMP WHERE id = ?`, id,
	)
	return err
}

// HasScope checks whether a key has the required scope.
func (k *APIKey) HasScope(scope string) bool {
	for _, s := range k.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

func sha256Key(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}
