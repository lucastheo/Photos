package httpapi

// LoginRequest represents the JSON body for POST /api/v1/auth/login.
type LoginRequest struct {
	Password string `json:"password"`
}

// SessionResponse represents the authentication session state returned by auth endpoints.
type SessionResponse struct {
	Authenticated bool `json:"authenticated"`
	AuthDisabled  bool `json:"auth_disabled"`
}
