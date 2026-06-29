package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/solarisjon/phoenix/internal/model"
)

const sessionCookieName = "phoenix_session"
const sessionDuration = 30 * 24 * time.Hour

type loginRequest struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || req.Password == "" {
		respondErr(w, http.StatusBadRequest, "name and password are required")
		return
	}

	user, err := s.users.GetByName(r.Context(), req.Name)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if user == nil {
		respondErr(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		respondErr(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	sess := &model.Session{
		ID:        uuid.New().String(),
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(sessionDuration),
		CreatedAt: time.Now(),
	}
	if err := s.sessions.Create(r.Context(), sess); err != nil {
		respondInternalErr(w, err)
		return
	}

	// Only mark the cookie Secure when the connection is actually HTTPS
	// (TLS terminated here or signalled by a reverse proxy). Plain HTTP deploys
	// (VPS without TLS) must not set Secure or the browser discards the cookie.
	secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sess.ID,
		Expires:  sess.ExpiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
		Path:     "/",
	})
	respond(w, http.StatusOK, map[string]string{"id": user.ID, "name": user.Name})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookieName)
	if err == nil {
		_ = s.sessions.DeleteByID(r.Context(), cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
	})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	if u == nil {
		respondErr(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	respond(w, http.StatusOK, map[string]string{"id": u.ID, "name": u.Name})
}

