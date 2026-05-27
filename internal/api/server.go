package api

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-playground/validator/v10"
	"github.com/sjfehlen/mediamesh/internal/audit"
	"github.com/sjfehlen/mediamesh/internal/auth"
	"github.com/sjfehlen/mediamesh/internal/catalog"
	"github.com/sjfehlen/mediamesh/internal/config"
	"github.com/sjfehlen/mediamesh/internal/identity"
	"github.com/sjfehlen/mediamesh/internal/peers"
	"github.com/sjfehlen/mediamesh/internal/requests"
	"github.com/sjfehlen/mediamesh/internal/transfers"
	"github.com/sjfehlen/mediamesh/internal/users"
	"github.com/sjfehlen/mediamesh/internal/webhooks"
)

// Server holds all service references.
type Server struct {
	cfg        *config.Config
	db         *sql.DB
	identity   *identity.Identity
	users      *users.Store
	auth       *auth.Manager
	scanner    *catalog.Scanner
	peers      *peers.Manager
	requests   *requests.Store
	transfers  *transfers.Engine
	audit      *audit.Log
	dispatcher *webhooks.Dispatcher
	validate   *validator.Validate
	hub        *Hub
}

// New creates a new API server.
func New(
	cfg *config.Config,
	db *sql.DB,
	id *identity.Identity,
	u *users.Store,
	a *auth.Manager,
	scanner *catalog.Scanner,
	p *peers.Manager,
	req *requests.Store,
	tr *transfers.Engine,
	al *audit.Log,
	dispatcher *webhooks.Dispatcher,
) *Server {
	hub := NewHub()
	tr.SetHub(hub)

	return &Server{
		cfg:        cfg,
		db:         db,
		identity:   id,
		users:      u,
		auth:       a,
		scanner:    scanner,
		peers:      p,
		requests:   req,
		transfers:  tr,
		audit:      al,
		dispatcher: dispatcher,
		validate:   validator.New(),
		hub:        hub,
	}
}

// Handler registers all routes and returns the http.Handler.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)

	// Start WebSocket hub.
	go s.hub.Run()

	// Auth routes — no session required.
	r.Post("/api/auth/login", s.handleLogin)
	r.Post("/api/auth/logout", s.handleLogout)
	r.Get("/api/auth/oidc", s.handleOIDCRedirect)
	r.Get("/api/auth/oidc/callback", s.handleOIDCCallback)

	// Bootstrap — only works when zero users exist.
	r.Post("/api/auth/bootstrap", s.handleBootstrap)

	// WebSocket — session required.
	r.With(s.auth.Middleware).Get("/api/ws", s.handleWS)

	// Peer-to-peer routes — peer JWT auth.
	r.Group(func(r chi.Router) {
		r.Use(s.peers.PeerMiddleware)
		r.Post("/api/peer/handshake", s.handlePeerHandshake)
		r.Post("/api/peer/catalog", s.handlePeerCatalog)
		r.Get("/api/peer/files/{itemID}", s.handlePeerFile)
	})

	// Session-authenticated routes.
	r.Group(func(r chi.Router) {
		r.Use(s.auth.Middleware)

		// Library.
		r.Get("/api/library", s.handleLibraryList)
		r.Get("/api/library/{id}", s.handleLibraryItem)

		// Requests.
		r.Get("/api/requests", s.handleRequestsList)
		r.Post("/api/requests", s.handleRequestSubmit)

		// Peers.
		r.Post("/api/peers/accept", s.handlePeerAccept)
		r.Get("/api/peers", s.handlePeersList)
	})

	// Admin-only routes.
	r.Group(func(r chi.Router) {
		r.Use(s.auth.RequireRole("admin"))

		r.Post("/api/requests/{id}/approve", s.handleRequestApprove)
		r.Post("/api/requests/{id}/reject", s.handleRequestReject)

		r.Get("/api/transfers", s.handleTransfersList)
		r.Post("/api/transfers/{id}/pause", s.handleTransferPause)
		r.Post("/api/transfers/{id}/resume", s.handleTransferResume)

		r.Post("/api/peers/invite", s.handlePeerInvite)
		r.Delete("/api/peers/{id}", s.handlePeerRevoke)

		r.Get("/api/users", s.handleUsersList)
		r.Post("/api/users", s.handleUserCreate)
		r.Patch("/api/users/{id}", s.handleUserUpdate)
		r.Delete("/api/users/{id}", s.handleUserDisable)
		r.Post("/api/users/invite", s.handleUserInvite)
		r.Get("/api/users/sessions", s.handleSessionsList)
		r.Delete("/api/users/sessions/{id}", s.handleSessionRevoke)

		r.Get("/api/audit", s.handleAuditList)

		// Webhook management.
		r.Get("/api/webhooks", s.handleWebhookList)
		r.Post("/api/webhooks", s.handleWebhookCreate)
		r.Delete("/api/webhooks/{id}", s.handleWebhookDelete)
		r.Patch("/api/webhooks/{id}/enabled", s.handleWebhookSetEnabled)
		r.Get("/api/webhooks/{id}/deliveries", s.handleWebhookDeliveries)

		// API key management.
		r.Get("/api/apikeys", s.handleAPIKeyList)
		r.Post("/api/apikeys", s.handleAPIKeyCreate)
		r.Delete("/api/apikeys/{id}", s.handleAPIKeyRevoke)
	})

	// API key authenticated routes — same scoped access as session auth but for machines.
	// Accepts "Authorization: Bearer mm_<key>" with scope enforcement.
	r.Group(func(r chi.Router) {
		r.Use(s.apiKeyMiddleware(webhooks.ScopeLibraryRead))
		r.Get("/api/v1/library", s.handleLibraryList)
		r.Get("/api/v1/library/{id}", s.handleLibraryItem)
	})
	r.Group(func(r chi.Router) {
		r.Use(s.apiKeyMiddleware(webhooks.ScopeRequestsRead))
		r.Get("/api/v1/requests", s.handleRequestsList)
	})
	r.Group(func(r chi.Router) {
		r.Use(s.apiKeyMiddleware(webhooks.ScopeRequestsWrite))
		r.Post("/api/v1/requests", s.handleRequestSubmit)
	})
	r.Group(func(r chi.Router) {
		r.Use(s.apiKeyMiddleware(webhooks.ScopeTransfersRead))
		r.Get("/api/v1/transfers", s.handleTransfersList)
	})

	return r
}

// apiKeyMiddleware authenticates requests using a Bearer API key and enforces a required scope.
func (s *Server) apiKeyMiddleware(requiredScope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := extractToken(r)
			if raw == "" || !strings.HasPrefix(raw, "mm_") {
				http.Error(w, "api key required", http.StatusUnauthorized)
				return
			}
			key, err := webhooks.ValidateAPIKey(r.Context(), s.db, raw)
			if err != nil {
				http.Error(w, "invalid or revoked api key", http.StatusUnauthorized)
				return
			}
			if !key.HasScope(requiredScope) {
				http.Error(w, "insufficient scope", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// --- Auth handlers ---

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username" validate:"required"`
		Password string `json:"password" validate:"required"`
	}
	if !decodeAndValidate(w, r, &req, s.validate) {
		return
	}
	u, err := s.users.GetByUsername(r.Context(), req.Username)
	if err != nil || !users.VerifyPassword(u.PasswordHash, req.Password) {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	if u.DisabledAt != nil {
		http.Error(w, "account disabled", http.StatusForbidden)
		return
	}
	token, err := s.auth.CreateSession(r.Context(), u.ID)
	if err != nil {
		slog.Error("create session", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"token": token})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	token := extractToken(r)
	if token != "" {
		_ = s.auth.RevokeSession(r.Context(), token)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleOIDCRedirect(w http.ResponseWriter, r *http.Request) {
	url := s.auth.OIDCAuthURL("state")
	if url == "" {
		http.Error(w, "OIDC not configured", http.StatusNotFound)
		return
	}
	http.Redirect(w, r, url, http.StatusFound)
}

func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	u, err := s.auth.OIDCCallback(r.Context(), code)
	if err != nil {
		http.Error(w, "oidc error: "+err.Error(), http.StatusBadRequest)
		return
	}
	token, err := s.auth.CreateSession(r.Context(), u.ID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"token": token})
}

// --- Library handlers ---

func (s *Server) handleLibraryList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	mediaType := q.Get("type")
	peerID := q.Get("peer_id")
	search := q.Get("search")

	items, err := catalog.GetAll(r.Context(), s.db)
	if err != nil {
		slog.Error("library list", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	var filtered []*catalog.Item
	for _, item := range items {
		if mediaType != "" && string(item.MediaType) != mediaType {
			continue
		}
		if peerID != "" {
			if item.PeerID == nil || *item.PeerID != peerID {
				continue
			}
		}
		if search != "" && !strings.Contains(strings.ToLower(item.Title), strings.ToLower(search)) {
			continue
		}
		filtered = append(filtered, item)
	}
	writeJSON(w, filtered)
}

func (s *Server) handleLibraryItem(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	item, err := catalog.GetByID(r.Context(), s.db, id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, item)
}

// --- Request handlers ---

func (s *Server) handleRequestsList(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	reqs, err := s.requests.List(r.Context(), u.ID, u.Role == "admin")
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, reqs)
}

func (s *Server) handleRequestSubmit(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	var body struct {
		ItemID string `json:"item_id" validate:"required"`
		Note   string `json:"note"`
	}
	if !decodeAndValidate(w, r, &body, s.validate) {
		return
	}
	req, err := s.requests.Submit(r.Context(), u.ID, body.ItemID, body.Note)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, req)
}

func (s *Server) handleRequestApprove(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	u := auth.UserFromContext(r.Context())
	var body struct{ Note string `json:"note"` }
	_ = json.NewDecoder(r.Body).Decode(&body)
	req, err := s.requests.Approve(r.Context(), id, u.ID, body.Note)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, req)
}

func (s *Server) handleRequestReject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	u := auth.UserFromContext(r.Context())
	var body struct{ Note string `json:"note"` }
	_ = json.NewDecoder(r.Body).Decode(&body)
	req, err := s.requests.Reject(r.Context(), id, u.ID, body.Note)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, req)
}

// --- Transfer handlers ---

func (s *Server) handleTransfersList(w http.ResponseWriter, r *http.Request) {
	list, err := s.transfers.List(r.Context())
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, list)
}

func (s *Server) handleTransferPause(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.transfers.Pause(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleTransferResume(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.transfers.Resume(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Peer handlers ---

func (s *Server) handlePeerInvite(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	token, err := s.peers.GenerateInvite(r.Context(), u.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"token": token})
}

func (s *Server) handlePeerAccept(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string `json:"token" validate:"required"`
	}
	if !decodeAndValidate(w, r, &body, s.validate) {
		return
	}
	peer, err := s.peers.AcceptInvite(r.Context(), body.Token)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, peer)
}

func (s *Server) handlePeersList(w http.ResponseWriter, r *http.Request) {
	list, err := s.peers.List(r.Context())
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, list)
}

func (s *Server) handlePeerRevoke(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.peers.Revoke(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Peer-to-peer handlers ---

func (s *Server) handlePeerHandshake(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PeerID      string `json:"peer_id"      validate:"required"`
		DisplayName string `json:"display_name" validate:"required"`
		Endpoint    string `json:"endpoint"     validate:"required,url"`
		PublicKey   string `json:"public_key"`
	}
	if !decodeAndValidate(w, r, &body, s.validate) {
		return
	}

	var pubKeyBytes []byte
	if body.PublicKey != "" {
		b, err := base64.StdEncoding.DecodeString(body.PublicKey)
		if err != nil {
			http.Error(w, "invalid public key", http.StatusBadRequest)
			return
		}
		pubKeyBytes = b
	}

	_, err := s.peers.Handshake(r.Context(), body.PeerID, body.DisplayName, body.Endpoint, pubKeyBytes)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]string{
		"peer_id":      s.identity.Fingerprint,
		"display_name": s.cfg.NodeName,
		"endpoint":     s.cfg.PublicURL,
		"public_key":   base64.StdEncoding.EncodeToString(s.identity.PublicKey),
	})
}

func (s *Server) handlePeerCatalog(w http.ResponseWriter, r *http.Request) {
	peer, err := s.peers.VerifyRequest(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var items []peers.CatalogItem
	if err := json.NewDecoder(r.Body).Decode(&items); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if err := s.peers.ReceiveCatalog(r.Context(), peer.ID, items); err != nil {
		slog.Error("receive catalog", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePeerFile(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "itemID")
	peers.StreamFile(s.db, []string{"/media/movies", "/media/tv", "/media/audiobooks", "/media/ebooks"}, itemID, w, r)
}

// --- User handlers ---

func (s *Server) handleUsersList(w http.ResponseWriter, r *http.Request) {
	list, err := s.users.List(r.Context())
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, list)
}

func (s *Server) handleUserCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username    string `json:"username"     validate:"required,min=2,max=64"`
		DisplayName string `json:"display_name" validate:"required"`
		Role        string `json:"role"         validate:"omitempty,oneof=member admin"`
		Password    string `json:"password"     validate:"required,min=8"`
	}
	if !decodeAndValidate(w, r, &body, s.validate) {
		return
	}
	if body.Role == "" {
		body.Role = "member"
	}
	u, err := s.users.Create(r.Context(), body.Username, body.DisplayName, body.Password, body.Role)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, u)
}

func (s *Server) handleUserUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct {
		Role        string `json:"role"         validate:"omitempty,oneof=member admin"`
		AutoApprove bool   `json:"auto_approve"`
		CanRequest  bool   `json:"can_request"`
		QuotaGB     *int64 `json:"quota_gb"`
	}
	if !decodeAndValidate(w, r, &body, s.validate) {
		return
	}
	if err := s.users.UpdateUser(r.Context(), id, body.Role, body.AutoApprove, body.CanRequest, body.QuotaGB); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleUserDisable(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.users.SetDisabled(r.Context(), id, true); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleUserInvite(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	token, err := s.users.GenerateInvite(r.Context(), u.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"token": token})
}

func (s *Server) handleSessionsList(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.auth.ListSessions(r.Context(), "")
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, sessions)
}

func (s *Server) handleSessionRevoke(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.auth.RevokeSession(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Audit handler ---

func (s *Server) handleAuditList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	actorID := q.Get("actor_id")
	action := q.Get("action")
	limitStr := q.Get("limit")

	var limit int
	if limitStr != "" {
		_, _ = fmt.Sscanf(limitStr, "%d", &limit)
	}
	if limit == 0 {
		limit = 100
	}

	var from, to time.Time
	if f := q.Get("from"); f != "" {
		from, _ = time.Parse(time.RFC3339, f)
	}
	if t := q.Get("to"); t != "" {
		to, _ = time.Parse(time.RFC3339, t)
	}

	entries, err := s.audit.List(r.Context(), actorID, action, from, to, limit)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, entries)
}

// --- Bootstrap handler ---

func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	// Only works when no users exist — becomes a no-op once any admin is created.
	var count int
	if err := s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if count > 0 {
		http.Error(w, "bootstrap unavailable: users already exist", http.StatusForbidden)
		return
	}

	var body struct {
		Username string `json:"username" validate:"required,min=2"`
		Password string `json:"password" validate:"required,min=8"`
	}
	if !decodeAndValidate(w, r, &body, s.validate) {
		return
	}

	u, err := s.users.Create(r.Context(), body.Username, body.Username, body.Password, "admin")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	token, err := s.auth.CreateSession(r.Context(), u.ID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	slog.Info("bootstrap admin created", "username", u.Username)
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, map[string]string{"token": token, "user_id": u.ID})
}

// --- Webhook handlers ---

func (s *Server) handleWebhookList(w http.ResponseWriter, r *http.Request) {
	list, err := webhooks.List(r.Context(), s.db)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, list)
}

func (s *Server) handleWebhookCreate(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	var body struct {
		Name   string   `json:"name"   validate:"required"`
		URL    string   `json:"url"    validate:"required,url"`
		Events []string `json:"events" validate:"required,min=1"`
	}
	if !decodeAndValidate(w, r, &body, s.validate) {
		return
	}
	wh, rawSecret, err := webhooks.Create(r.Context(), s.db, body.Name, body.URL, u.ID, body.Events)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Return secret only on creation.
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, map[string]any{"webhook": wh, "secret": rawSecret})
}

func (s *Server) handleWebhookDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := webhooks.Delete(r.Context(), s.db, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleWebhookSetEnabled(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := webhooks.SetEnabled(r.Context(), s.db, id, body.Enabled); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleWebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	deliveries, err := webhooks.Deliveries(r.Context(), s.db, id, 50)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, deliveries)
}

// --- API key handlers ---

func (s *Server) handleAPIKeyList(w http.ResponseWriter, r *http.Request) {
	keys, err := webhooks.ListAPIKeys(r.Context(), s.db)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, keys)
}

func (s *Server) handleAPIKeyCreate(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFromContext(r.Context())
	var body struct {
		Name   string   `json:"name"   validate:"required"`
		Scopes []string `json:"scopes" validate:"required,min=1"`
	}
	if !decodeAndValidate(w, r, &body, s.validate) {
		return
	}
	key, rawKey, err := webhooks.CreateAPIKey(r.Context(), s.db, body.Name, u.ID, body.Scopes)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, map[string]any{"key": key, "raw_key": rawKey})
}

func (s *Server) handleAPIKeyRevoke(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := webhooks.RevokeAPIKey(r.Context(), s.db, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Helpers ---

// decodeAndValidate decodes JSON from r.Body into v and validates struct tags.
// Returns false and writes an error response if decoding or validation fails.
func decodeAndValidate(w http.ResponseWriter, r *http.Request, v interface{}, validate *validator.Validate) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return false
	}
	if err := validate.Struct(v); err != nil {
		http.Error(w, "validation error: "+err.Error(), http.StatusUnprocessableEntity)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("write json", "err", err)
	}
}

func extractToken(r *http.Request) string {
	a := r.Header.Get("Authorization")
	if strings.HasPrefix(a, "Bearer ") {
		return strings.TrimPrefix(a, "Bearer ")
	}
	if c, err := r.Cookie("session"); err == nil {
		return c.Value
	}
	return ""
}
