package registry

import (
	"context"
	"net/http"
)

// ExchangeResult is the PAT exchange response: a short-lived JWT plus the
// PAT's record id (used by logout to revoke without a stored id).
type ExchangeResult struct {
	Token   string `json:"token"`
	Expires string `json:"expires"`
	ID      string `json:"id"`
}

// ExchangePAT swaps a personal access token for a short-lived auth JWT.
func (c *Client) ExchangePAT(ctx context.Context, pat string) (ExchangeResult, error) {
	var out ExchangeResult
	err := c.doJSON(ctx, http.MethodPost, c.baseURL+"/api/registry/auth/pat/exchange", "",
		map[string]string{"token": pat}, &out)
	return out, err
}

// RevokePAT deletes a token by id (used by logout to revoke the stored
// credential server-side; token management otherwise lives in the web UI).
func (c *Client) RevokePAT(ctx context.Context, id string) error {
	auth, err := c.bearer(ctx)
	if err != nil {
		return err
	}
	return c.doJSON(ctx, http.MethodDelete, c.baseURL+"/api/registry/auth/pat/"+id, auth, nil, nil)
}
