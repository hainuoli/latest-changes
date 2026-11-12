package github

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"reflect"
	"strconv"
	"time"

	"github.com/hainuoli/latest-changes/internal/model"
)

var (
	// LabelPermissions scopes classification to repository contents, PR labels and statuses.
	LabelPermissions = map[string]string{
		"contents":      "read",
		"pull_requests": "write",
		"statuses":      "write",
	}
	// ContentsPermissions scopes release-note updates to repository files.
	ContentsPermissions = map[string]string{
		"contents": "write",
	}
)

// CreateAppJWT signs a short-lived App JWT for the supplied time.
func (c *Client) CreateAppJWT(now time.Time) (string, error) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, _ := json.Marshal(struct {
		Issuer    string `json:"iss"`
		IssuedAt  int64  `json:"iat"`
		ExpiresAt int64  `json:"exp"`
	}{
		Issuer:    c.clientID,
		IssuedAt:  now.Add(-time.Minute).Unix(),
		ExpiresAt: now.Add(9 * time.Minute).Unix(),
	})
	unsigned := header + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, c.key, crypto.SHA256, digest[:])
	if err != nil {
		return "", errors.New("could not sign App JWT")
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

// CreateInstallationToken verifies repository, permissions, and expiry before returning a token.
func (c *Client) CreateInstallationToken(ctx context.Context, repo model.Repository, permissions map[string]string) (string, error) {
	if err := repo.Validate(); err != nil {
		return "", err
	}
	jwt, err := c.CreateAppJWT(time.Now())
	if err != nil {
		return "", err
	}
	var installation struct {
		ID int64 `json:"id"`
	}
	if err := c.request(ctx, "GET", repoPath(repo)+"/installation", jwt, nil, &installation); err != nil {
		return "", err
	}
	if installation.ID <= 0 {
		return "", errors.New("invalid installation response")
	}
	var result struct {
		Token        string            `json:"token"`
		Expires      time.Time         `json:"expires_at"`
		Permissions  map[string]string `json:"permissions"`
		Selection    string            `json:"repository_selection"`
		Repositories []struct {
			ID int64 `json:"id"`
		} `json:"repositories"`
	}
	payload := map[string]any{
		"repository_ids": []int64{repo.ID},
		"permissions":    permissions,
	}
	endpoint := "/app/installations/" + strconv.FormatInt(installation.ID, 10) + "/access_tokens"
	if err := c.request(ctx, "POST", endpoint, jwt, payload, &result); err != nil {
		return "", err
	}
	expected := map[string]string{"metadata": "read"}
	for key, value := range permissions {
		expected[key] = value
	}
	if result.Token == "" || !result.Expires.After(time.Now()) || result.Selection != "selected" ||
		len(result.Repositories) != 1 || result.Repositories[0].ID != repo.ID ||
		!reflect.DeepEqual(expected, result.Permissions) {
		return "", errors.New("installation token has invalid scope or expiry")
	}
	return result.Token, nil
}

func parsePrivateKey(privateKey string) (*rsa.PrivateKey, error) {
	block, rest := pem.Decode([]byte(privateKey))
	if block == nil || len(bytes.TrimSpace(rest)) != 0 {
		return nil, errors.New("invalid private key PEM")
	}
	var key *rsa.PrivateKey
	switch block.Type {
	case "RSA PRIVATE KEY":
		parsed, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, errors.New("invalid RSA private key")
		}
		key = parsed
	case "PRIVATE KEY":
		parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, errors.New("invalid private key")
		}
		var ok bool
		key, ok = parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("private key must use RSA")
		}
	default:
		return nil, errors.New("unsupported private key format")
	}
	if err := key.Validate(); err != nil || key.N.BitLen() < 2048 {
		return nil, errors.New("invalid RSA key or key shorter than 2048 bits")
	}

	return key, nil
}
