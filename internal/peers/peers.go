package peers

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/sjfehlen/mediamesh/internal/audit"
	"github.com/sjfehlen/mediamesh/internal/config"
	"github.com/sjfehlen/mediamesh/internal/identity"
)

// Peer maps a peers table row.
type Peer struct {
	ID          string     `json:"id"`
	DisplayName string     `json:"display_name"`
	Endpoint    string     `json:"endpoint"`
	PublicKey   []byte     `json:"public_key,omitempty"`
	Status      string     `json:"status"`
	AddedAt     time.Time  `json:"added_at"`
	LastSeen    *time.Time `json:"last_seen,omitempty"`
	MissedPings int        `json:"-"`
}

// Manager manages peer relationships.
type Manager struct {
	db       *sql.DB
	identity *identity.Identity
	cfg      *config.Config
	audit    *audit.Log

	mu       sync.Mutex
	seenJTIs map[string]time.Time // jti -> expiry, for replay prevention
}

// New creates a new peer Manager.
func New(db *sql.DB, id *identity.Identity, cfg *config.Config, a *audit.Log) *Manager {
	m := &Manager{
		db:       db,
		identity: id,
		cfg:      cfg,
		audit:    a,
		seenJTIs: make(map[string]time.Time),
	}
	go m.cleanJTIs()
	return m
}

// GenerateInvite creates a peer invite JWT signed by this node's private key.
func (m *Manager) GenerateInvite(ctx context.Context, createdBy string) (string, error) {
	jti := uuid.New().String()
	exp := time.Now().UTC().Add(24 * time.Hour)

	pubB64 := base64.StdEncoding.EncodeToString(m.identity.PublicKey)
	claims := jwt.MapClaims{
		"iss":        m.identity.Fingerprint,
		"endpoint":   m.cfg.PublicURL,
		"public_key": pubB64,
		"jti":        jti,
		"exp":        exp.Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	signed, err := token.SignedString(m.identity.PrivateKey)
	if err != nil {
		return "", fmt.Errorf("sign peer invite: %w", err)
	}

	_, err = m.db.ExecContext(ctx,
		`INSERT INTO invite_tokens (token, created_by, token_type, expires_at) VALUES (?, ?, 'peer', ?)`,
		jti, createdBy, exp,
	)
	if err != nil {
		return "", fmt.Errorf("store peer invite: %w", err)
	}

	_ = m.audit.Write(ctx, audit.Entry{
		ActorID:    createdBy,
		ActorType:  "user",
		Action:     "peer.invite_created",
		TargetType: "peer",
	})

	return signed, nil
}

// AcceptInvite parses a peer invite JWT, verifies it, and completes the handshake.
// If endpointOverride is non-empty it takes precedence over the endpoint embedded in the token.
func (m *Manager) AcceptInvite(ctx context.Context, tokenStr, endpointOverride string) (*Peer, error) {
	// Parse without verification first to extract the embedded public key.
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	unverified, _, err := parser.ParseUnverified(tokenStr, jwt.MapClaims{})
	if err != nil {
		return nil, fmt.Errorf("parse invite token: %w", err)
	}

	claims, ok := unverified.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("invalid claims")
	}

	pubB64, _ := claims["public_key"].(string)
	endpoint, _ := claims["endpoint"].(string)
	if endpointOverride != "" {
		endpoint = endpointOverride
	}
	issuer, _ := claims["iss"].(string)

	pubKeyBytes, err := base64.StdEncoding.DecodeString(pubB64)
	if err != nil {
		return nil, fmt.Errorf("decode public key: %w", err)
	}
	pubKey := ed25519.PublicKey(pubKeyBytes)

	// Now verify the signature.
	verified, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodEd25519); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return pubKey, nil
	})
	if err != nil || !verified.Valid {
		return nil, fmt.Errorf("invalid invite token signature: %w", err)
	}

	// Call remote handshake.
	peer, err := m.callHandshake(ctx, endpoint, pubKey)
	if err != nil {
		return nil, fmt.Errorf("handshake failed: %w", err)
	}

	// Store peer with the issuer's fingerprint as ID.
	peer.ID = issuer
	peer.PublicKey = pubKeyBytes
	peer.Endpoint = endpoint

	if err := m.storePeer(ctx, peer); err != nil {
		return nil, fmt.Errorf("store peer: %w", err)
	}

	_ = m.audit.Write(ctx, audit.Entry{
		ActorType:  "system",
		Action:     "peer.accepted",
		TargetType: "peer",
		TargetID:   peer.ID,
		Detail:     endpoint,
	})

	// Kick off an initial catalog exchange in the background so both sides
	// see each other's media without waiting for the next scheduled sync.
	// Small delay gives the remote node time to finish storing this peer before
	// we send authenticated requests.
	go func() {
		time.Sleep(2 * time.Second)
		bgCtx := context.Background()
		slog.Info("starting initial catalog sync", "peer", peer.ID, "endpoint", peer.Endpoint)
		if err := m.PushCatalog(bgCtx, peer); err != nil {
			slog.Error("initial catalog push failed", "peer", peer.ID, "err", err)
			_ = m.audit.Write(bgCtx, audit.Entry{
				ActorType: "system", Action: "catalog.push_failed",
				TargetType: "peer", TargetID: peer.ID,
				Detail: err.Error(), ErrorCode: audit.ErrSyncPushFailed,
			})
		} else {
			slog.Info("initial catalog push done", "peer", peer.ID)
		}
		if err := m.PullCatalog(bgCtx, peer); err != nil {
			slog.Error("initial catalog pull failed", "peer", peer.ID, "err", err)
			_ = m.audit.Write(bgCtx, audit.Entry{
				ActorType: "system", Action: "catalog.pull_failed",
				TargetType: "peer", TargetID: peer.ID,
				Detail: err.Error(), ErrorCode: audit.ErrSyncPullFailed,
			})
		} else {
			slog.Info("initial catalog pull done", "peer", peer.ID)
		}
	}()

	return peer, nil
}

type handshakeRequest struct {
	PeerID      string `json:"peer_id"`
	DisplayName string `json:"display_name"`
	Endpoint    string `json:"endpoint"`
	PublicKey   string `json:"public_key"`
}

func (m *Manager) callHandshake(ctx context.Context, endpoint string, _ ed25519.PublicKey) (*Peer, error) {
	body, _ := json.Marshal(handshakeRequest{
		PeerID:      m.identity.Fingerprint,
		DisplayName: m.cfg.NodeName,
		Endpoint:    m.cfg.PublicURL,
		PublicKey:   base64.StdEncoding.EncodeToString(m.identity.PublicKey),
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/api/peer/handshake", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if err := m.SignRequest(req); err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("handshake status %d: %s", resp.StatusCode, respBody)
	}

	var result handshakeRequest
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	pubBytes, _ := base64.StdEncoding.DecodeString(result.PublicKey)
	return &Peer{
		ID:          result.PeerID,
		DisplayName: result.DisplayName,
		Endpoint:    result.Endpoint,
		PublicKey:   pubBytes,
		Status:      "active",
	}, nil
}

// Handshake is called when a remote node completes the handshake.
func (m *Manager) Handshake(ctx context.Context, peerID, displayName, endpoint string, pubKey ed25519.PublicKey) (*Peer, error) {
	peer := &Peer{
		ID:          peerID,
		DisplayName: displayName,
		Endpoint:    endpoint,
		PublicKey:   pubKey,
		Status:      "active",
	}
	if err := m.storePeer(ctx, peer); err != nil {
		return nil, fmt.Errorf("store peer from handshake: %w", err)
	}
	_ = m.audit.Write(ctx, audit.Entry{
		ActorType:  "peer",
		Action:     "peer.handshake",
		TargetType: "peer",
		TargetID:   peerID,
	})
	return peer, nil
}

func (m *Manager) storePeer(ctx context.Context, p *Peer) error {
	_, err := m.db.ExecContext(ctx,
		`INSERT INTO peers (id, display_name, endpoint, public_key, status, added_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET display_name=excluded.display_name, endpoint=excluded.endpoint, status=excluded.status, missed_pings=0`,
		p.ID, p.DisplayName, p.Endpoint, p.PublicKey, p.Status, time.Now().UTC(),
	)
	return err
}

// List returns all peers.
func (m *Manager) List(ctx context.Context) ([]*Peer, error) {
	rows, err := m.db.QueryContext(ctx,
		`SELECT id, display_name, endpoint, public_key, status, added_at, last_seen, missed_pings FROM peers`)
	if err != nil {
		return nil, fmt.Errorf("query peers: %w", err)
	}
	defer rows.Close()

	var peers []*Peer
	for rows.Next() {
		p := &Peer{}
		if err := rows.Scan(&p.ID, &p.DisplayName, &p.Endpoint, &p.PublicKey, &p.Status, &p.AddedAt, &p.LastSeen, &p.MissedPings); err != nil {
			return nil, err
		}
		peers = append(peers, p)
	}
	return peers, rows.Err()
}

// Revoke removes a peer from the registry.
func (m *Manager) Revoke(ctx context.Context, peerID string) error {
	_, err := m.db.ExecContext(ctx, `DELETE FROM peers WHERE id = ?`, peerID)
	if err != nil {
		return err
	}
	_ = m.audit.Write(ctx, audit.Entry{
		ActorType:  "system",
		Action:     "peer.removed",
		TargetType: "peer",
		TargetID:   peerID,
	})
	return nil
}

// SignRequest adds a signed JWT Bearer token to an outbound request.
func (m *Manager) SignRequest(req *http.Request) error {
	jti := uuid.New().String()
	claims := jwt.MapClaims{
		"iss": m.identity.Fingerprint,
		"jti": jti,
		"exp": time.Now().UTC().Add(5 * time.Minute).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	signed, err := token.SignedString(m.identity.PrivateKey)
	if err != nil {
		return fmt.Errorf("sign request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+signed)
	return nil
}

// VerifyRequest parses and verifies a peer-signed request.
func (m *Manager) VerifyRequest(r *http.Request) (*Peer, error) {
	auth := r.Header.Get("Authorization")
	if len(auth) < 8 {
		return nil, fmt.Errorf("missing authorization header")
	}
	tokenStr := auth[7:]

	// Extract issuer (fingerprint) without verifying to look up the peer.
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	unverified, _, err := parser.ParseUnverified(tokenStr, jwt.MapClaims{})
	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}

	claims, ok := unverified.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("invalid claims")
	}
	fingerprint, _ := claims["iss"].(string)
	jti, _ := claims["jti"].(string)

	peer, err := m.getByFingerprint(r.Context(), fingerprint)
	if err != nil {
		return nil, fmt.Errorf("peer not found: %w", err)
	}
	if peer.Status != "active" {
		return nil, fmt.Errorf("peer not active")
	}

	pubKey := ed25519.PublicKey(peer.PublicKey)
	verified, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodEd25519); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return pubKey, nil
	})
	if err != nil || !verified.Valid {
		return nil, fmt.Errorf("invalid peer token: %w", err)
	}

	// Replay prevention.
	if err := m.checkJTI(jti, time.Now().Add(5*time.Minute)); err != nil {
		return nil, err
	}

	return peer, nil
}

func (m *Manager) getByFingerprint(ctx context.Context, fp string) (*Peer, error) {
	p := &Peer{}
	err := m.db.QueryRowContext(ctx,
		`SELECT id, display_name, endpoint, public_key, status, added_at, last_seen, missed_pings FROM peers WHERE id = ?`, fp,
	).Scan(&p.ID, &p.DisplayName, &p.Endpoint, &p.PublicKey, &p.Status, &p.AddedAt, &p.LastSeen, &p.MissedPings)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("peer not found")
	}
	return p, err
}

func (m *Manager) checkJTI(jti string, exp time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, seen := m.seenJTIs[jti]; seen {
		return fmt.Errorf("replay detected: jti already seen")
	}
	m.seenJTIs[jti] = exp
	return nil
}

func (m *Manager) cleanJTIs() {
	for range time.Tick(time.Minute) {
		now := time.Now()
		m.mu.Lock()
		for jti, exp := range m.seenJTIs {
			if now.After(exp) {
				delete(m.seenJTIs, jti)
			}
		}
		m.mu.Unlock()
	}
}

// StartHeartbeat starts a background goroutine that pings all active/unreachable peers every 60 seconds.
func (m *Manager) StartHeartbeat(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.pingAllPeers(ctx)
			}
		}
	}()
}

// pingAllPeers pings all active and unreachable peers and updates their status.
func (m *Manager) pingAllPeers(ctx context.Context) {
	rows, err := m.db.QueryContext(ctx,
		`SELECT id, display_name, endpoint, public_key, status, added_at, last_seen, missed_pings
		 FROM peers WHERE status IN ('active', 'unreachable')`)
	if err != nil {
		slog.Error("peers.pingAllPeers: query peers", "err", err)
		return
	}
	defer rows.Close()

	var peerList []*Peer
	for rows.Next() {
		p := &Peer{}
		if err := rows.Scan(&p.ID, &p.DisplayName, &p.Endpoint, &p.PublicKey, &p.Status, &p.AddedAt, &p.LastSeen, &p.MissedPings); err != nil {
			slog.Error("peers.pingAllPeers: scan peer", "err", err)
			continue
		}
		peerList = append(peerList, p)
	}
	if err := rows.Err(); err != nil {
		slog.Error("peers.pingAllPeers: rows error", "err", err)
		return
	}

	for _, p := range peerList {
		pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := m.pingPeer(pingCtx, p)
		cancel()

		now := time.Now().UTC()
		if err == nil {
			slog.Debug("peers.pingAllPeers: peer reachable", "peer_id", p.ID)
			_, dbErr := m.db.ExecContext(ctx,
				`UPDATE peers SET last_seen = ?, missed_pings = 0, status = 'active' WHERE id = ?`,
				now, p.ID,
			)
			if dbErr != nil {
				slog.Error("peers.pingAllPeers: update peer on success", "peer_id", p.ID, "err", dbErr)
			}
		} else {
			newMissed := p.MissedPings + 1
			if newMissed > 3 {
				newMissed = 3
			}
			newStatus := p.Status
			if newMissed >= 3 {
				newStatus = "unreachable"
			}
			if newStatus == "unreachable" && p.Status != "unreachable" {
				slog.Info("peers.pingAllPeers: peer unreachable", "peer_id", p.ID, "endpoint", p.Endpoint)
				_ = m.audit.Write(ctx, audit.Entry{
					ActorType:  "system",
					Action:     "peer.unreachable",
					TargetType: "peer",
					TargetID:   p.ID,
					Detail:     p.Endpoint,
					ErrorCode:  audit.ErrPeerUnreachable,
				})
			}
			_, dbErr := m.db.ExecContext(ctx,
				`UPDATE peers SET missed_pings = ?, status = ? WHERE id = ?`,
				newMissed, newStatus, p.ID,
			)
			if dbErr != nil {
				slog.Error("peers.pingAllPeers: update peer on failure", "peer_id", p.ID, "err", dbErr)
			}
		}
	}
}

func (m *Manager) pingPeer(ctx context.Context, p *Peer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.Endpoint+"/api/peer/ping", nil)
	if err != nil {
		return fmt.Errorf("peers.pingPeer: create request: %w", err)
	}
	if err := m.SignRequest(req); err != nil {
		return fmt.Errorf("peers.pingPeer: sign request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("peers.pingPeer: do request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("peers.pingPeer: unexpected status %d", resp.StatusCode)
	}
	return nil
}

// PeerMiddleware authenticates peer-to-peer requests.
func (m *Manager) PeerMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := m.VerifyRequest(r)
		if err != nil {
			slog.Debug("peer auth failed", "err", err)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
