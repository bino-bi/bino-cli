package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// AuthResult is a successful password/TOTP authentication: a session JWT.
type AuthResult struct {
	Token string `json:"token"`
}

// MFARequiredError reports that password auth succeeded but the account
// requires a TOTP second step; MfaID identifies the pending session.
type MFARequiredError struct {
	MfaID string
}

func (e *MFARequiredError) Error() string {
	return "multi-factor authentication required"
}

// ExchangeResult is the PAT exchange response: a short-lived JWT plus the
// PAT's record id (used by logout to revoke without a stored id).
type ExchangeResult struct {
	Token   string `json:"token"`
	Expires string `json:"expires"`
	ID      string `json:"id"`
}

// PATCreated is the create response; Token is the plaintext, shown once.
type PATCreated struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Token   string `json:"token"`
	Prefix  string `json:"prefix"`
	Expires string `json:"expires"`
	Created string `json:"created"`
}

// PATInfo is one token in the list response (no secrets).
type PATInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Prefix   string `json:"prefix"`
	Created  string `json:"created"`
	LastUsed string `json:"lastUsed"`
	Expires  string `json:"expires"`
}

// AuthWithPassword authenticates against the registry's user store. An
// account with TOTP enabled answers with a pending MFA session, surfaced as
// *MFARequiredError.
func (c *Client) AuthWithPassword(ctx context.Context, identity, password string) (AuthResult, error) {
	payload, err := json.Marshal(map[string]string{"identity": identity, "password": password})
	if err != nil {
		return AuthResult{}, err
	}
	body, _, status, err := c.doOnce(ctx, http.MethodPost, c.baseURL+"/api/collections/users/auth-with-password", payload, "")
	if err != nil {
		return AuthResult{}, err
	}
	if status == http.StatusUnauthorized {
		var mfa struct {
			MfaID string `json:"mfaId"`
		}
		if json.Unmarshal(body, &mfa) == nil && mfa.MfaID != "" {
			return AuthResult{}, &MFARequiredError{MfaID: mfa.MfaID}
		}
	}
	if status < 200 || status > 299 {
		apiErr := &APIError{Status: status}
		if jsonErr := json.Unmarshal(body, apiErr); jsonErr != nil || apiErr.Code == "" {
			apiErr.Code = fmt.Sprintf("http_%d", status)
		}
		return AuthResult{}, apiErr
	}
	var out AuthResult
	if err := json.Unmarshal(body, &out); err != nil || out.Token == "" {
		return AuthResult{}, fmt.Errorf("registry: unexpected auth response")
	}
	return out, nil
}

// VerifyTOTP completes a pending MFA session with an authenticator code.
func (c *Client) VerifyTOTP(ctx context.Context, mfaID, code string) (AuthResult, error) {
	var out AuthResult
	err := c.doJSON(ctx, http.MethodPost, c.baseURL+"/api/registry/auth/totp/verify", "",
		map[string]string{"mfaId": mfaID, "code": code}, &out)
	return out, err
}

// ExchangePAT swaps a personal access token for a short-lived auth JWT.
func (c *Client) ExchangePAT(ctx context.Context, pat string) (ExchangeResult, error) {
	var out ExchangeResult
	err := c.doJSON(ctx, http.MethodPost, c.baseURL+"/api/registry/auth/pat/exchange", "",
		map[string]string{"token": pat}, &out)
	return out, err
}

// CreatePAT mints a new token. jwt authenticates the call; when empty, the
// client's own credential (via bearer) is used.
func (c *Client) CreatePAT(ctx context.Context, jwt, name string, expiresIn int64) (PATCreated, error) {
	var out PATCreated
	auth := jwt
	if auth == "" {
		var err error
		if auth, err = c.bearer(ctx); err != nil {
			return out, err
		}
	}
	err := c.doJSON(ctx, http.MethodPost, c.baseURL+"/api/registry/auth/pat", auth,
		map[string]any{"name": name, "expiresIn": expiresIn}, &out)
	return out, err
}

// ListPATs returns the caller's tokens.
func (c *Client) ListPATs(ctx context.Context) ([]PATInfo, error) {
	auth, err := c.bearer(ctx)
	if err != nil {
		return nil, err
	}
	var out struct {
		Items []PATInfo `json:"items"`
	}
	if err := c.doJSON(ctx, http.MethodGet, c.baseURL+"/api/registry/auth/pat", auth, nil, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

// RevokePAT deletes a token by id.
func (c *Client) RevokePAT(ctx context.Context, id string) error {
	auth, err := c.bearer(ctx)
	if err != nil {
		return err
	}
	return c.doJSON(ctx, http.MethodDelete, c.baseURL+"/api/registry/auth/pat/"+id, auth, nil, nil)
}
