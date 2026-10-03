package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"github.com/na-cho-dev/go-event-api/internal/database"
)

// RegisterRequest creates an account.
type RegisterRequest struct {
	Email    string `json:"email" binding:"required,email,max=254" example:"ada@example.com"`
	Password string `json:"password" binding:"required,min=8,max=72" example:"correct horse battery"`
	Name     string `json:"name" binding:"required,min=2,max=100" example:"Ada Lovelace"`
}

// LoginRequest exchanges credentials for a token.
type LoginRequest struct {
	Email    string `json:"email" binding:"required,email" example:"ada@example.com"`
	Password string `json:"password" binding:"required" example:"correct horse battery"`
}

// AuthResponse carries a signed token and the account it belongs to.
type AuthResponse struct {
	Token     string        `json:"token"`
	ExpiresAt time.Time     `json:"expiresAt"`
	User      database.User `json:"user"`
}

func normaliseEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func (s *Server) authResponse(c *gin.Context, status int, user *database.User) {
	token, exp, err := s.tokens.Issue(user.ID)
	if err != nil {
		s.internalError(c, "issue token", err)
		return
	}
	c.JSON(status, AuthResponse{Token: token, ExpiresAt: exp, User: *user})
}

// register creates a user and returns a token for it.
//
//	@Summary	Register
//	@Tags		Auth
//	@Accept		json
//	@Produce	json
//	@Param		body	body		RegisterRequest	true	"Account details"
//	@Success	201		{object}	AuthResponse
//	@Failure	400		{object}	ErrorResponse
//	@Failure	409		{object}	ErrorResponse
//	@Router		/api/v1/auth/register [post]
func (s *Server) register(c *gin.Context) {
	var req RegisterRequest
	if !bindJSON(c, &req) {
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), s.cfg.BcryptCost)
	if err != nil {
		s.internalError(c, "hash password", err)
		return
	}

	user := &database.User{
		Email:    normaliseEmail(req.Email),
		Name:     strings.TrimSpace(req.Name),
		Password: string(hash),
	}
	if err := s.stores.Users.Insert(c.Request.Context(), user); err != nil {
		if errors.Is(err, database.ErrDuplicate) {
			fail(c, http.StatusConflict, CodeConflict, "An account with this email already exists.")
			return
		}
		s.internalError(c, "insert user", err)
		return
	}

	s.authResponse(c, http.StatusCreated, user)
}

// login verifies credentials and returns a token.
//
//	@Summary	Log in
//	@Tags		Auth
//	@Accept		json
//	@Produce	json
//	@Param		body	body		LoginRequest	true	"Credentials"
//	@Success	200		{object}	AuthResponse
//	@Failure	400		{object}	ErrorResponse
//	@Failure	401		{object}	ErrorResponse
//	@Router		/api/v1/auth/login [post]
func (s *Server) login(c *gin.Context) {
	var req LoginRequest
	if !bindJSON(c, &req) {
		return
	}

	user, err := s.stores.Users.GetByEmail(c.Request.Context(), normaliseEmail(req.Email))
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		s.internalError(c, "load user by email", err)
		return
	}

	// Compare against a dummy hash when the account is unknown so the
	// response time does not reveal which emails are registered.
	hash := s.dummyHash
	if user != nil {
		hash = []byte(user.Password)
	}
	if bcrypt.CompareHashAndPassword(hash, []byte(req.Password)) != nil || user == nil {
		fail(c, http.StatusUnauthorized, CodeInvalidCredentials, "The email or password is incorrect.")
		return
	}

	s.authResponse(c, http.StatusOK, user)
}

// me returns the account behind the token.
//
//	@Summary	Current user
//	@Tags		Auth
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	database.User
//	@Failure	401	{object}	ErrorResponse
//	@Router		/api/v1/auth/me [get]
func (s *Server) me(c *gin.Context) {
	c.JSON(http.StatusOK, currentUser(c))
}
