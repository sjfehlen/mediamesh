package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/sjfehlen/mediamesh/internal/config"
	"github.com/sjfehlen/mediamesh/internal/users"
	"golang.org/x/oauth2"
)

type contextKey string

const userContextKey contextKey = "auth_user"

// Manager handles sessions, middleware, and OIDC.
type Manager struct {
	db       *sql.DB
	users    *users.Store
	cfg      *config.Config
	provider *gooidc.Provider
	oauth2   *oauth2.Config
}

// New creates an auth manager. If OIDCIssuer is configured, sets up the provider.
func New(ctx context.Context, db *sql.DB, u *users.Store, cfg *config.Config) (*Manager, error) {
	m := &Manager{db: db, users: u, cfg: cfg}

	if cfg.OIDCIssuer != "" {
		provider, err := gooidc.NewProvider(ctx, cfg.OIDCIssuer)
		if err != nil {
			return nil, fmt.Errorf("create oidc provider: %w", err)
		}
		m.provider = provider
		m.oauth2 = &oauth2.Config{
			ClientID:     cfg.OIDCClientID,
			ClientSecret: cfg.OIDCClientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  cfg.PublicURL + "/api/auth/oidc/callback",
			Scopes:       []string{gooidc.ScopeOpenID, "profile", "email"},
		}
	}

	return m, nil
}

// CreateSession creates a new session token for the given user.
func (m *Manager) CreateSession(ctx context.Context, userID string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	token := hex.EncodeToString(b)
	id := token // use token as session ID

	_, err := m.db.ExecContext(ctx,
		`INSERT INTO sessions (id, user_id, expires_at) VALUES (?, ?, ?)`,
		id, userID, time.Now().UTC().Add(30*24*time.Hour),
	)
	if err != nil {
		return "", fmt.Errorf("insert session: %w", err)
	}
	return token, nil
}

// ValidateSession looks up a session token and returns the associated user.
func (m *Manager) ValidateSession(ctx context.Context, token string) (*users.User, error) {
	var userID string
	var expiresAt time.Time
	var revokedAt *time.Time

	err := m.db.QueryRowContext(ctx,
		`SELECT user_id, expires_at, revoked_at FROM sessions WHERE id = ?`, token,
	).Scan(&userID, &expiresAt, &revokedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("session not found")
	}
	if err != nil {
		return nil, fmt.Errorf("query session: %w", err)
	}
	if revokedAt != nil {
		return nil, fmt.Errorf("session revoked")
	}
	if time.Now().UTC().After(expiresAt) {
		return nil, fmt.Errorf("session expired")
	}

	_, _ = m.db.ExecContext(ctx,
		`UPDATE sessions SET last_seen = ? WHERE id = ?`, time.Now().UTC(), token)

	return m.users.GetByID(ctx, userID)
}

// RevokeSession marks a session as revoked.
func (m *Manager) RevokeSession(ctx context.Context, token string) error {
	_, err := m.db.ExecContext(ctx,
		`UPDATE sessions SET revoked_at = ? WHERE id = ?`, time.Now().UTC(), token)
	return err
}

// UserFromContext extracts the authenticated user from context.
func UserFromContext(ctx context.Context) *users.User {
	u, _ := ctx.Value(userContextKey).(*users.User)
	return u
}

// Middleware extracts a Bearer token or session cookie and validates it.
// If missing/invalid, it returns 401.
func (m *Manager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := extractToken(r)
		if token == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		u, err := m.ValidateSession(r.Context(), token)
		if err != nil {
			slog.Debug("session invalid", "err", err)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if u.DisabledAt != nil {
			http.Error(w, "account disabled", http.StatusForbidden)
			return
		}
		ctx := context.WithValue(r.Context(), userContextKey, u)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireRole returns a middleware that enforces a minimum role.
func (m *Manager) RequireRole(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := UserFromContext(r.Context())
			if u == nil || !hasRole(u.Role, role) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		}))
	}
}

// OIDCAuthURL returns the OIDC authorization URL for the given state.
func (m *Manager) OIDCAuthURL(state string) string {
	if m.oauth2 == nil {
		return ""
	}
	return m.oauth2.AuthCodeURL(state)
}

// OIDCCallback exchanges the OIDC code for a user.
func (m *Manager) OIDCCallback(ctx context.Context, code string) (*users.User, error) {
	if m.oauth2 == nil || m.provider == nil {
		return nil, fmt.Errorf("OIDC not configured")
	}
	token, err := m.oauth2.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("exchange oidc code: %w", err)
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		return nil, fmt.Errorf("no id_token in response")
	}

	verifier := m.provider.Verifier(&gooidc.Config{ClientID: m.cfg.OIDCClientID})
	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("verify id_token: %w", err)
	}

	var claims struct {
		Sub  string `json:"sub"`
		Name string `json:"name"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("parse claims: %w", err)
	}

	return m.users.UpsertOIDC(ctx, claims.Sub, m.cfg.OIDCIssuer, claims.Name)
}

// ListSessions returns sessions for a user (or all if userID is empty).
func (m *Manager) ListSessions(ctx context.Context, userID string) ([]SessionRow, error) {
	query := `SELECT id, user_id, created_at, last_seen, expires_at, revoked_at FROM sessions WHERE revoked_at IS NULL`
	args := []interface{}{}
	if userID != "" {
		query += " AND user_id = ?"
		args = append(args, userID)
	}
	rows, err := m.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query sessions: %w", err)
	}
	defer rows.Close()

	var sessions []SessionRow
	for rows.Next() {
		var s SessionRow
		if err := rows.Scan(&s.ID, &s.UserID, &s.CreatedAt, &s.LastSeen, &s.ExpiresAt, &s.RevokedAt); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

// SessionRow is a session row from the DB.
type SessionRow struct {
	ID        string
	UserID    string
	CreatedAt time.Time
	LastSeen  time.Time
	ExpiresAt time.Time
	RevokedAt *time.Time
}

func extractToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	if c, err := r.Cookie("session"); err == nil {
		return c.Value
	}
	return ""
}

func hasRole(userRole, required string) bool {
	roles := map[string]int{"member": 1, "admin": 2}
	return roles[userRole] >= roles[required]
}
