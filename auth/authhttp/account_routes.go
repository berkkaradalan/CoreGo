package authhttp

import (
	"net/http"

	"github.com/berkkaradalan/CoreGo/auth"
	"github.com/berkkaradalan/CoreGo/web"
)

// forgotPassword returns the handler of POST /forgot-password. Like
// POST /verify-email/request it answers 202 with the same body for any
// address and whether or not a mailer is configured, so it cannot be
// used to enumerate accounts. Requests also count against perEmail (nil
// when rate limiting is disabled), which limits registered and unknown
// addresses alike.
func (h *Handlers) forgotPassword(perEmail *web.RateLimiter) web.HandlerFunc {
	return func(c *web.Context) error {
		var req emailRequest
		if err := c.BindJSON(&req); err != nil {
			return mapError(err)
		}
		if err := h.allowEmail(c, perEmail, req.Email); err != nil {
			return err
		}
		if err := h.service.RequestPasswordReset(c, req.Email); err != nil {
			return mapError(err)
		}
		return c.JSON(http.StatusAccepted, messageResponse{Message: msgEmailRequestAccepted})
	}
}

// resetPassword handles POST /reset-password, called by the
// application's "choose a new password" page with the email and token
// from the reset link. A successful reset revokes every session of the
// user.
func (h *Handlers) resetPassword(c *web.Context) error {
	var req resetPasswordRequest
	if err := c.BindJSON(&req); err != nil {
		return mapError(err)
	}
	if err := h.service.ResetPassword(c, req.Email, req.Token, req.NewPassword); err != nil {
		return mapError(err)
	}
	return c.NoContent()
}

// changePassword handles POST /change-password. Changing the password
// revokes every session of the user, the current one included, so the
// cookie is cleared too and the client signs in again.
func (h *Handlers) changePassword(c *web.Context) error {
	var req changePasswordRequest
	if err := c.BindJSON(&req); err != nil {
		return mapError(err)
	}
	_, user, err := currentAuth(c)
	if err != nil {
		return err
	}
	if err := h.service.ChangePassword(c, user.ID, req.CurrentPassword, req.NewPassword); err != nil {
		return mapError(err)
	}
	h.clearSessionCookie(c)
	return c.NoContent()
}

// changeEmail handles POST /change-email. The new address starts out
// unverified; sessions are kept.
func (h *Handlers) changeEmail(c *web.Context) error {
	var req changeEmailRequest
	if err := c.BindJSON(&req); err != nil {
		return mapError(err)
	}
	_, user, err := currentAuth(c)
	if err != nil {
		return err
	}
	updated, err := h.service.ChangeEmail(c, user.ID, req.NewEmail, req.CurrentPassword)
	if err != nil {
		return mapError(err)
	}
	return c.JSON(http.StatusOK, userEnvelope{User: newUserResponse(updated)})
}

// getMe handles GET /me with the user RequireAuth loaded.
func (h *Handlers) getMe(c *web.Context) error {
	_, user, err := currentAuth(c)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, userEnvelope{User: newUserResponse(user)})
}

// updateMe handles PATCH /me: only the fields present in the body
// change. The email address is a credential and changes through
// POST /change-email, so a body containing "email" is rejected instead
// of being silently ignored.
func (h *Handlers) updateMe(c *web.Context) error {
	var req updateMeRequest
	if err := c.BindJSON(&req); err != nil {
		return mapError(err)
	}
	if req.Email != nil {
		return web.BadRequest(msgEmailNotPatchable)
	}
	_, user, err := currentAuth(c)
	if err != nil {
		return err
	}
	updated, err := h.service.UpdateUser(c, user.ID, auth.UpdateUserInput{
		Name:  req.Name,
		Image: req.Image,
	})
	if err != nil {
		return mapError(err)
	}
	return c.JSON(http.StatusOK, userEnvelope{User: newUserResponse(updated)})
}

// deleteMe handles DELETE /me. The password is confirmed first, so a
// stolen session token alone cannot delete the account. Deleting the
// user removes its sessions, so the cookie is cleared too.
func (h *Handlers) deleteMe(c *web.Context) error {
	var req deleteMeRequest
	if err := c.BindJSON(&req); err != nil {
		return mapError(err)
	}
	if req.Password == "" {
		return mapError(auth.ErrPasswordRequired)
	}
	_, user, err := currentAuth(c)
	if err != nil {
		return err
	}
	if err := h.service.CheckPassword(c, user.ID, req.Password); err != nil {
		return mapError(err)
	}
	if err := h.service.DeleteUser(c, user.ID); err != nil {
		return mapError(err)
	}
	h.clearSessionCookie(c)
	return c.NoContent()
}
