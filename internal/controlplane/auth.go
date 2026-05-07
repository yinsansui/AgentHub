package controlplane

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"agenthub/pkg/protocol"
)

const (
	authCookieName      = "agenthub_session"
	authSessionDuration = 24 * time.Hour
	adminUserID         = "admin"
)

type User struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

type AuthSession struct {
	ID        string
	User      User
	ExpiresAt time.Time
}

type authContextKey struct{}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !validBuiltinCredentials(req.Username, req.Password) {
		http.Error(w, "invalid username or password", http.StatusUnauthorized)
		return
	}
	sessionID, err := newAuthSessionID()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	user := builtinAdminUser()
	expiresAt := time.Now().UTC().Add(authSessionDuration)
	s.mu.Lock()
	s.authSessions[sessionID] = AuthSession{ID: sessionID, User: user, ExpiresAt: expiresAt}
	s.mu.Unlock()
	http.SetCookie(w, authCookie(sessionID, expiresAt))
	writeJSON(w, map[string]any{"user": user})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(authCookieName); err == nil {
		s.mu.Lock()
		delete(s.authSessions, cookie.Value)
		s.mu.Unlock()
	}
	http.SetCookie(w, expiredAuthCookie())
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, map[string]any{"user": user})
}

func (s *Server) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isPublicRoute(r) {
			next.ServeHTTP(w, r)
			return
		}
		user, ok := s.userFromCookie(r)
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), authContextKey{}, user)))
	})
}

func isPublicRoute(r *http.Request) bool {
	if r.Method == http.MethodGet && r.URL.Path == "/health" {
		return true
	}
	if r.Method == http.MethodPost && (r.URL.Path == "/auth/login" || r.URL.Path == "/auth/logout") {
		return true
	}
	return false
}

func (s *Server) userFromCookie(r *http.Request) (User, bool) {
	cookie, err := r.Cookie(authCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return User{}, false
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.authSessions[cookie.Value]
	if !ok || !session.ExpiresAt.After(now) {
		delete(s.authSessions, cookie.Value)
		return User{}, false
	}
	return session.User, true
}

func currentUser(ctx context.Context) (User, bool) {
	user, ok := ctx.Value(authContextKey{}).(User)
	return user, ok
}

func currentUserID(ctx context.Context) string {
	user, ok := currentUser(ctx)
	if !ok {
		return ""
	}
	return user.ID
}

func builtinAdminUser() User {
	return User{ID: adminUserID, Username: "admin", Role: "admin"}
}

func validBuiltinCredentials(username, password string) bool {
	usernameOK := subtle.ConstantTimeCompare([]byte(strings.TrimSpace(username)), []byte("admin")) == 1
	passwordOK := subtle.ConstantTimeCompare([]byte(password), []byte("admin")) == 1
	return usernameOK && passwordOK
}

func newAuthSessionID() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func authCookie(sessionID string, expiresAt time.Time) *http.Cookie {
	return &http.Cookie{
		Name:     authCookieName,
		Value:    sessionID,
		Path:     "/",
		Expires:  expiresAt,
		MaxAge:   int(time.Until(expiresAt).Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
}

func expiredAuthCookie() *http.Cookie {
	return &http.Cookie{
		Name:     authCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
}

func workspaceIDFromRequest(r *http.Request) (string, error) {
	workspaceID := r.PathValue("workspaceId")
	if err := validateSafeSegment(workspaceID, "workspaceId"); err != nil {
		return "", err
	}
	return workspaceID, nil
}

func (s *Server) existingWorkspaceIDFromRequest(r *http.Request) (string, bool, error) {
	workspaceID, err := workspaceIDFromRequest(r)
	if err != nil {
		return "", false, err
	}
	_, ok, err := s.store.GetWorkspace(r.Context(), currentUserID(r.Context()), workspaceID)
	return workspaceID, ok, err
}

func (s *Server) sessionForCurrentUser(r *http.Request, sessionID string) (SessionProjection, bool, error) {
	session, ok, err := s.store.GetSession(r.Context(), sessionID)
	if err != nil || !ok {
		return session, ok, err
	}
	_, ok, err = s.store.GetWorkspace(r.Context(), currentUserID(r.Context()), session.WorkspaceID)
	if err != nil || !ok {
		return SessionProjection{}, ok, err
	}
	return session, true, nil
}

func sanitizeWorkspaceForUser(userID string, workspace WorkspaceProjection) WorkspaceProjection {
	return workspace
}

func sanitizeWorkspacesForUser(userID string, workspaces []WorkspaceProjection) []WorkspaceProjection {
	out := make([]WorkspaceProjection, 0, len(workspaces))
	for _, workspace := range workspaces {
		out = append(out, sanitizeWorkspaceForUser(userID, workspace))
	}
	return out
}

func sanitizeTaskForUser(userID string, task TaskProjection) TaskProjection {
	return task
}

func sanitizeSessionForUser(userID string, session SessionProjection) SessionProjection {
	return session
}

func sanitizeSessionsForUser(userID string, sessions []SessionProjection) []SessionProjection {
	out := make([]SessionProjection, 0, len(sessions))
	for _, session := range sessions {
		out = append(out, sanitizeSessionForUser(userID, session))
	}
	return out
}

func sanitizeRunForUser(userID string, run SessionRun) SessionRun {
	return run
}

func sanitizeRunPointerForUser(userID string, run *SessionRun) *SessionRun {
	if run == nil {
		return nil
	}
	sanitized := sanitizeRunForUser(userID, *run)
	return &sanitized
}

func sanitizeConnectionForUser(userID string, connection LLMConnection) LLMConnection {
	connection.UserID = userID
	return connection
}

func sanitizeSkillForUser(userID string, skill SkillDefinitionWithFiles) SkillDefinitionWithFiles {
	return skill
}

func sanitizeSkillsForUser(userID string, skills []SkillDefinitionWithFiles) []SkillDefinitionWithFiles {
	out := make([]SkillDefinitionWithFiles, 0, len(skills))
	for _, skill := range skills {
		out = append(out, sanitizeSkillForUser(userID, skill))
	}
	return out
}

func sanitizeMCPServerForUser(userID string, server MCPServerDefinitionWithEnv) MCPServerDefinitionWithEnv {
	return server
}

func sanitizeMCPServersForUser(userID string, servers []MCPServerDefinitionWithEnv) []MCPServerDefinitionWithEnv {
	out := make([]MCPServerDefinitionWithEnv, 0, len(servers))
	for _, server := range servers {
		out = append(out, sanitizeMCPServerForUser(userID, server))
	}
	return out
}

func sanitizeMessageForUser(userID string, message MessageProjection) MessageProjection {
	return message
}

func sanitizeMessagesForUser(userID string, messages []MessageProjection) []MessageProjection {
	out := make([]MessageProjection, 0, len(messages))
	for _, message := range messages {
		out = append(out, sanitizeMessageForUser(userID, message))
	}
	return out
}

func sanitizeEventForUser(userID string, event StoredEvent) StoredEvent {
	event.Payload = sanitizeProtocolEventForUser(userID, event.Payload)
	return event
}

func sanitizeEventsForUser(userID string, events []StoredEvent) []StoredEvent {
	out := make([]StoredEvent, 0, len(events))
	for _, event := range events {
		out = append(out, sanitizeEventForUser(userID, event))
	}
	return out
}

func sanitizeProtocolEventForUser(userID string, event protocol.UniversalEvent) protocol.UniversalEvent {
	return event
}

func sanitizeStateForUser(userID string, state SessionState) SessionState {
	state.Messages = sanitizeMessagesForUser(userID, state.Messages)
	state.ActiveRun = sanitizeRunPointerForUser(userID, state.ActiveRun)
	return state
}
