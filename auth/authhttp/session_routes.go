package authhttp

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/berkkaradalan/CoreGo/auth"
	"github.com/berkkaradalan/CoreGo/web"
)

// maxUserAgentBytes caps the User-Agent stored on a session; headers can
// be far larger than anything worth persisting.
const maxUserAgentBytes = 512

// userAgent returns the request's User-Agent, truncated to
// maxUserAgentBytes on a UTF-8 boundary.
func userAgent(c *web.Context) string {
	ua := c.GetHeader("User-Agent")
	if len(ua) <= maxUserAgentBytes {
		return ua
	}
	cut := maxUserAgentBytes
	for cut > 0 && !utf8.RuneStart(ua[cut]) {
		cut--
	}
	return strings.ToValidUTF8(ua[:cut], "")
}

// bodyToken returns the session token to put in the JSON body of the
// sign-up and sign-in responses: token itself, or "" (omitting the
// field) when CookieConfig.OmitTokenFromBody keeps it in the cookie
// only.
func (h *Handlers) bodyToken(token string) string {
	if h.cfg.Cookie.Enabled && h.cfg.Cookie.OmitTokenFromBody {
		return ""
	}
	return token
}

// signUp handles POST /signup.
func (h *Handlers) signUp(c *web.Context) error {
	var req signUpRequest
	if err := c.BindJSON(&req); err != nil {
		return mapError(err)
	}
	out, err := h.service.SignUp(c, auth.SignUpInput{
		Name:       req.Name,
		Email:      req.Email,
		Password:   req.Password,
		Image:      req.Image,
		RememberMe: req.RememberMe,
		IPAddress:  c.RemoteIP(),
		UserAgent:  userAgent(c),
	})
	if err != nil {
		return mapError(err)
	}
	resp := signUpResponse{User: newUserResponse(out.User)}
	if out.Session == nil || out.SessionToken == "" {
		// The service requires a verified address before signing in.
		resp.VerificationRequired = true
		return c.JSON(http.StatusCreated, resp)
	}
	expires := out.Session.ExpiresAt
	resp.Token = h.bodyToken(out.SessionToken)
	resp.ExpiresAt = &expires
	h.setSessionCookie(c, out.SessionToken, expires)
	return c.JSON(http.StatusCreated, resp)
}

// signIn returns the handler of POST /signin. Besides the per client
// limiter installed by Mount, every attempt counts against perEmail (nil
// when rate limiting is disabled), so password guessing against one
// account is limited no matter how many addresses the attacker uses.
func (h *Handlers) signIn(perEmail *web.RateLimiter) web.HandlerFunc {
	return func(c *web.Context) error {
		var req signInRequest
		if err := c.BindJSON(&req); err != nil {
			return mapError(err)
		}
		if err := h.allowEmail(c, perEmail, req.Email); err != nil {
			return err
		}
		out, err := h.service.SignIn(c, auth.SignInInput{
			Email:      req.Email,
			Password:   req.Password,
			RememberMe: req.RememberMe,
			IPAddress:  c.RemoteIP(),
			UserAgent:  userAgent(c),
		})
		if err != nil {
			return mapError(err)
		}
		h.setSessionCookie(c, out.SessionToken, out.Session.ExpiresAt)
		return c.JSON(http.StatusOK, signInResponse{
			User:      newUserResponse(out.User),
			Token:     h.bodyToken(out.SessionToken),
			ExpiresAt: out.Session.ExpiresAt,
		})
	}
}

// signOut handles POST /signout: it ends the session the request is
// authenticated with.
func (h *Handlers) signOut(c *web.Context) error {
	token, ok := h.Token(c.Request())
	if !ok {
		return errMissingToken()
	}
	if err := h.service.SignOut(c, token); err != nil {
		return mapError(err)
	}
	h.clearSessionCookie(c)
	return c.NoContent()
}

// currentSession handles GET /session.
func (h *Handlers) currentSession(c *web.Context) error {
	session, user, err := currentAuth(c)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, sessionDetailResponse{
		User:    newUserResponse(user),
		Session: newSessionResponse(session, session.ID),
	})
}

// listSessions handles GET /sessions.
func (h *Handlers) listSessions(c *web.Context) error {
	current, user, err := currentAuth(c)
	if err != nil {
		return err
	}
	sessions, err := h.service.ListSessions(c, user.ID)
	if err != nil {
		return mapError(err)
	}
	out := make([]sessionResponse, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, newSessionResponse(s, current.ID))
	}
	return c.JSON(http.StatusOK, listSessionsResponse{Sessions: out})
}

// revokeAllSessions handles DELETE /sessions: it revokes every session
// of the user, the current one included, so the cookie is cleared too.
func (h *Handlers) revokeAllSessions(c *web.Context) error {
	_, user, err := currentAuth(c)
	if err != nil {
		return err
	}
	if err := h.service.RevokeAllSessions(c, user.ID); err != nil {
		return mapError(err)
	}
	h.clearSessionCookie(c)
	return c.NoContent()
}

// revokeSession handles DELETE /sessions/:id. The service reports
// unknown IDs and other users' sessions alike as not found, which this
// route answers 404 (a missing resource) rather than the 401 used for
// an unusable session token. Revoking the current session clears the
// cookie.
func (h *Handlers) revokeSession(c *web.Context) error {
	current, user, err := currentAuth(c)
	if err != nil {
		return err
	}
	id := c.Param("id")
	if err := h.service.RevokeSession(c, user.ID, id); err != nil {
		if errors.Is(err, auth.ErrSessionNotFound) {
			return &web.HTTPError{Code: http.StatusNotFound, Message: msgSessionResourceNotFound, Err: err}
		}
		return mapError(err)
	}
	if id == current.ID {
		h.clearSessionCookie(c)
	}
	return c.NoContent()
}
