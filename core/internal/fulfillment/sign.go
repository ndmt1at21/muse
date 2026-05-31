package fulfillment

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// SignHMAC returns the lowercase hex HMAC-SHA256 of body under secret. The
// outbound n8n POST sends it as X-Muse-Signature; the orchestrator verifies it
// to trust the payload, and the inbound callback is signed the same way (and
// verified at the admin BFF edge).
func SignHMAC(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
