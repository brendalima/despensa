// Package whatsapp is the WhatsApp Cloud API behind the bot's two
// ports. Nothing here knows what a pantry is: it checks a signature,
// turns Meta's payload into messages, and sends text back.
package whatsapp

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/brendalima/despensa/internal/bot"
)

var _ bot.Webhook = (*Webhook)(nil)

// ErrSignature is a delivery that is not Meta's. The handler answers
// 403 and reads nothing: without this check, the webhook is a public
// write API for the household's pantry.
var ErrSignature = errors.New("the payload signature does not match")

// signatureHeader carries an HMAC-SHA256 of the raw body, hex encoded,
// prefixed with the algorithm.
const (
	signatureHeader = "X-Hub-Signature-256"
	signaturePrefix = "sha256="
)

type Webhook struct {
	verifyToken string
	appSecret   string
}

func NewWebhook(verifyToken, appSecret string) (*Webhook, error) {
	if verifyToken == "" {
		return nil, errors.New("missing whatsapp verify token")
	}
	if appSecret == "" {
		return nil, errors.New("missing whatsapp app secret")
	}
	return &Webhook{verifyToken: verifyToken, appSecret: appSecret}, nil
}

func MustNewWebhook(verifyToken, appSecret string) *Webhook {
	w, err := NewWebhook(verifyToken, appSecret)
	if err != nil {
		panic(fmt.Errorf("couldn't create the whatsapp webhook: %w", err))
	}
	return w
}

// Challenge answers Meta's subscription handshake: the token it sends
// has to be the one configured, and the challenge is echoed back as
// plain text.
func (w *Webhook) Challenge(r *http.Request) (string, error) {
	query := r.URL.Query()
	if query.Get("hub.mode") != "subscribe" {
		return "", errors.New("unknown hub mode")
	}

	token := query.Get("hub.verify_token")
	if subtle.ConstantTimeCompare([]byte(token), []byte(w.verifyToken)) != 1 {
		return "", errors.New("wrong verify token")
	}

	challenge := query.Get("hub.challenge")
	if challenge == "" {
		return "", errors.New("missing hub challenge")
	}
	return challenge, nil
}

// Verify checks the signature over the bytes that arrived. It compares
// in constant time, and it fails closed: a missing header, a malformed
// one and a wrong digest are all the same answer.
func (w *Webhook) Verify(r *http.Request, body []byte) error {
	header := r.Header.Get(signatureHeader)
	hex64, ok := strings.CutPrefix(header, signaturePrefix)
	if !ok {
		return ErrSignature
	}

	sent, err := hex.DecodeString(hex64)
	if err != nil {
		return ErrSignature
	}

	mac := hmac.New(sha256.New, []byte(w.appSecret))
	mac.Write(body)
	if !hmac.Equal(sent, mac.Sum(nil)) {
		return ErrSignature
	}
	return nil
}
