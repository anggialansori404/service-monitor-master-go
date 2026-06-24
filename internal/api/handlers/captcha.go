package handlers

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// captchaChallengeResponse is the JSON payload returned to the client
// for a local CAPTCHA challenge request.
type captchaChallengeResponse struct {
	A       int    `json:"a"`
	B       int    `json:"b"`
	Token   string `json:"token"`   // HMAC-signed token containing answer + expiry
	Expires int64  `json:"expires"` // unix timestamp
}

// localCaptchaClaims is embedded inside the HMAC-signed token.
type localCaptchaClaims struct {
	Answer int   `json:"answer"`
	Exp    int64 `json:"exp"`
	Nonce  int64 `json:"nonce"`
}

// turnstileVerifyResponse mirrors Cloudflare's siteverify response.
type turnstileVerifyResponse struct {
	Success    bool     `json:"success"`
	ErrorCodes []string `json:"error-codes"`
	Hostname   string   `json:"hostname"`
}

// cryptRandInt returns a cryptographically random integer in [min, max].
func cryptRandInt(min, max int) int {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(max-min+1)))
	if err != nil {
		// Fallback to time-based — extremely unlikely path
		return min + int(time.Now().UnixNano())%(max-min+1)
	}
	return min + int(n.Int64())
}

// signToken creates a base64-encoded HMAC-SHA256 token from claims.
func signToken(claims localCaptchaClaims, secret string) (string, error) {
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("marshal claims: %w", err)
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	sig := mac.Sum(nil)

	// Format: base64(payload).base64(signature)
	token := base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(sig)
	return token, nil
}

// verifyLocalCaptchaToken decodes and validates an HMAC-signed local captcha token.
func verifyLocalCaptchaToken(token, secret string) (*localCaptchaClaims, error) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid token format")
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("decode payload: %w", err)
	}
	sigBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode signature: %w", err)
	}

	// Verify HMAC
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payloadBytes)
	expectedSig := mac.Sum(nil)
	if !hmac.Equal(sigBytes, expectedSig) {
		return nil, fmt.Errorf("token signature mismatch")
	}

	var claims localCaptchaClaims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return nil, fmt.Errorf("unmarshal claims: %w", err)
	}

	// Check expiry (3-minute window)
	if time.Now().Unix() > claims.Exp {
		return nil, fmt.Errorf("token expired")
	}

	return &claims, nil
}

// verifyTurnstileToken calls the Cloudflare Turnstile siteverify endpoint.
func verifyTurnstileToken(token, secretKey string) (bool, error) {
	if secretKey == "" {
		return false, fmt.Errorf("turnstile secret key not configured")
	}

	data := url.Values{}
	data.Set("secret", secretKey)
	data.Set("response", token)

	resp, err := http.PostForm("https://challenges.cloudflare.com/turnstile/v0/siteverify", data)
	if err != nil {
		return false, fmt.Errorf("siteverify request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("read siteverify response: %w", err)
	}

	var result turnstileVerifyResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return false, fmt.Errorf("parse siteverify response: %w", err)
	}

	if !result.Success {
		errMsg := "verification failed"
		if len(result.ErrorCodes) > 0 {
			errMsg = strings.Join(result.ErrorCodes, ", ")
		}
		return false, fmt.Errorf("turnstile: %s", errMsg)
	}

	return true, nil
}

// GetCaptchaChallenge generates a local math challenge with HMAC-signed token.
func (h *FrontendHandler) GetCaptchaChallenge(c *gin.Context) {
	a := cryptRandInt(10, 99)
	b := cryptRandInt(10, 99)
	exp := time.Now().Add(3 * time.Minute).Unix()

	claims := localCaptchaClaims{
		Answer: a + b,
		Exp:    exp,
		Nonce:  time.Now().UnixNano(),
	}

	token, err := signToken(claims, h.CaptchaSecret)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate challenge"})
		return
	}

	c.JSON(http.StatusOK, captchaChallengeResponse{
		A:       a,
		B:       b,
		Token:   token,
		Expires: exp,
	})
}
