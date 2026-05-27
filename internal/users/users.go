package users

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sjfehlen/mediamesh/internal/audit"
	"golang.org/x/crypto/argon2"
)

// User maps all columns from the users table.
type User struct {
	ID          string
	Username    string
	DisplayName string
	PasswordHash string
	Role        string
	AutoApprove bool
	CanRequest  bool
	QuotaGB     *int64
	Libraries   *string
	CreatedAt   time.Time
	DisabledAt  *time.Time
	OIDCSub     *string
	OIDCIssuer  *string
}

// Store provides user management backed by SQLite.
type Store struct {
	db    *sql.DB
	audit *audit.Log
}

// NewStore creates a new user store.
func NewStore(db *sql.DB, a *audit.Log) *Store {
	return &Store{db: db, audit: a}
}

// Create creates a new local user with an argon2id-hashed password.
func (s *Store) Create(ctx context.Context, username, displayName, password, role string) (*User, error) {
	hash, err := hashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	id := uuid.New().String()
	now := time.Now().UTC()
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO users (id, username, display_name, password_hash, role, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		id, username, displayName, hash, role, now,
	)
	if err != nil {
		return nil, fmt.Errorf("insert user: %w", err)
	}

	_ = s.audit.Write(ctx, audit.Entry{
		ActorID:    id,
		ActorType:  "system",
		Action:     "user.create",
		TargetType: "user",
		TargetID:   id,
		Detail:     username,
	})

	return s.GetByID(ctx, id)
}

// GetByUsername looks up a user by username.
func (s *Store) GetByUsername(ctx context.Context, username string) (*User, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, username, display_name, password_hash, role, auto_approve, can_request,
		        quota_gb, libraries, created_at, disabled_at, oidc_sub, oidc_issuer
		 FROM users WHERE username = ?`, username)
	return scanUser(row)
}

// GetByID looks up a user by ID.
func (s *Store) GetByID(ctx context.Context, id string) (*User, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, username, display_name, password_hash, role, auto_approve, can_request,
		        quota_gb, libraries, created_at, disabled_at, oidc_sub, oidc_issuer
		 FROM users WHERE id = ?`, id)
	return scanUser(row)
}

// GetByOIDCSub looks up a user by OIDC subject.
func (s *Store) GetByOIDCSub(ctx context.Context, sub string) (*User, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, username, display_name, password_hash, role, auto_approve, can_request,
		        quota_gb, libraries, created_at, disabled_at, oidc_sub, oidc_issuer
		 FROM users WHERE oidc_sub = ?`, sub)
	return scanUser(row)
}

// UpsertOIDC creates or updates an OIDC user.
func (s *Store) UpsertOIDC(ctx context.Context, sub, issuer, displayName string) (*User, error) {
	existing, err := s.GetByOIDCSub(ctx, sub)
	if err == nil {
		// Update display name.
		_, err = s.db.ExecContext(ctx,
			`UPDATE users SET display_name = ?, oidc_issuer = ? WHERE id = ?`,
			displayName, issuer, existing.ID)
		if err != nil {
			return nil, fmt.Errorf("update oidc user: %w", err)
		}
		return s.GetByID(ctx, existing.ID)
	}

	// Create new OIDC user.
	id := uuid.New().String()
	username := "oidc_" + sub[:min(len(sub), 16)]
	now := time.Now().UTC()
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO users (id, username, display_name, password_hash, role, oidc_sub, oidc_issuer, created_at)
		 VALUES (?, ?, ?, '', 'member', ?, ?, ?)`,
		id, username, displayName, sub, issuer, now,
	)
	if err != nil {
		return nil, fmt.Errorf("insert oidc user: %w", err)
	}

	_ = s.audit.Write(ctx, audit.Entry{
		ActorType:  "oidc",
		Action:     "user.oidc_create",
		TargetType: "user",
		TargetID:   id,
		Detail:     sub,
	})

	return s.GetByID(ctx, id)
}

// List returns all users.
func (s *Store) List(ctx context.Context) ([]*User, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, username, display_name, password_hash, role, auto_approve, can_request,
		        quota_gb, libraries, created_at, disabled_at, oidc_sub, oidc_issuer
		 FROM users ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("query users: %w", err)
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		u, err := scanUserRow(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// SetDisabled enables or disables a user.
func (s *Store) SetDisabled(ctx context.Context, id string, disabled bool) error {
	var disabledAt interface{}
	if disabled {
		disabledAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `UPDATE users SET disabled_at = ? WHERE id = ?`, disabledAt, id)
	if err != nil {
		return fmt.Errorf("set disabled: %w", err)
	}
	action := "user.enable"
	if disabled {
		action = "user.disable"
	}
	_ = s.audit.Write(ctx, audit.Entry{
		ActorType:  "system",
		Action:     action,
		TargetType: "user",
		TargetID:   id,
	})
	return nil
}

// GenerateInvite creates a user invite token valid for 72 hours.
func (s *Store) GenerateInvite(ctx context.Context, createdBy string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	token := hex.EncodeToString(b)

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO invite_tokens (token, created_by, token_type, expires_at)
		 VALUES (?, ?, 'user', ?)`,
		token, createdBy, time.Now().UTC().Add(72*time.Hour),
	)
	if err != nil {
		return "", fmt.Errorf("insert invite token: %w", err)
	}

	_ = s.audit.Write(ctx, audit.Entry{
		ActorID:    createdBy,
		ActorType:  "user",
		Action:     "invite.create",
		TargetType: "invite_token",
		TargetID:   token[:8] + "...",
	})

	return token, nil
}

// UseInvite validates and consumes an invite token.
func (s *Store) UseInvite(ctx context.Context, token string) error {
	var expiresAt time.Time
	var usedAt *time.Time
	err := s.db.QueryRowContext(ctx,
		`SELECT expires_at, used_at FROM invite_tokens WHERE token = ? AND token_type = 'user'`,
		token,
	).Scan(&expiresAt, &usedAt)
	if err == sql.ErrNoRows {
		return fmt.Errorf("invite token not found")
	}
	if err != nil {
		return fmt.Errorf("query invite token: %w", err)
	}
	if usedAt != nil {
		return fmt.Errorf("invite token already used")
	}
	if time.Now().UTC().After(expiresAt) {
		return fmt.Errorf("invite token expired")
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE invite_tokens SET used_at = ? WHERE token = ?`,
		time.Now().UTC(), token,
	)
	return err
}

// VerifyPassword checks a plaintext password against an argon2id hash.
// Hash format: "argon2id$<salt_hex>$<hash_hex>"
func VerifyPassword(hash, password string) bool {
	parts := strings.SplitN(hash, "$", 3)
	if len(parts) != 3 || parts[0] != "argon2id" {
		return false
	}
	salt, err := hex.DecodeString(parts[1])
	if err != nil {
		return false
	}
	expected, err := hex.DecodeString(parts[2])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, 1, 64*1024, 4, 32)
	return constantTimeEqual(got, expected)
}

func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, 1, 64*1024, 4, 32)
	return fmt.Sprintf("argon2id$%s$%s", hex.EncodeToString(salt), hex.EncodeToString(hash)), nil
}

func constantTimeEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := range a {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

func scanUser(row *sql.Row) (*User, error) {
	u := &User{}
	err := row.Scan(
		&u.ID, &u.Username, &u.DisplayName, &u.PasswordHash,
		&u.Role, &u.AutoApprove, &u.CanRequest,
		&u.QuotaGB, &u.Libraries, &u.CreatedAt, &u.DisabledAt,
		&u.OIDCSub, &u.OIDCIssuer,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("user not found")
	}
	if err != nil {
		return nil, fmt.Errorf("scan user: %w", err)
	}
	return u, nil
}

type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanUserRow(row rowScanner) (*User, error) {
	u := &User{}
	err := row.Scan(
		&u.ID, &u.Username, &u.DisplayName, &u.PasswordHash,
		&u.Role, &u.AutoApprove, &u.CanRequest,
		&u.QuotaGB, &u.Libraries, &u.CreatedAt, &u.DisabledAt,
		&u.OIDCSub, &u.OIDCIssuer,
	)
	if err != nil {
		return nil, fmt.Errorf("scan user row: %w", err)
	}
	return u, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// UpdateUser updates mutable user fields.
func (s *Store) UpdateUser(ctx context.Context, id, role string, autoApprove, canRequest bool, quotaGB *int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET role = ?, auto_approve = ?, can_request = ?, quota_gb = ? WHERE id = ?`,
		role, autoApprove, canRequest, quotaGB, id,
	)
	if err != nil {
		return fmt.Errorf("update user: %w", err)
	}
	_ = s.audit.Write(ctx, audit.Entry{
		ActorType:  "system",
		Action:     "user.update",
		TargetType: "user",
		TargetID:   id,
	})
	return nil
}

// LogError is a helper to log errors without crashing.
func LogError(msg string, err error) {
	if err != nil {
		slog.Error(msg, "err", err)
	}
}
