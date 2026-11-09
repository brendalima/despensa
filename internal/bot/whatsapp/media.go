package whatsapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/brendalima/despensa/internal/bot"
)

var _ bot.MediaFetcher = (*Media)(nil)

const (
	// maxMediaBytes is Meta's own ceiling for audio, used here as the
	// read cap: anything larger is not a voice note about groceries,
	// and a body read without a limit is how a webhook runs a host out
	// of memory.
	maxMediaBytes = 16 << 20

	// mediaTimeout covers both hops. The whole message has two
	// minutes, and the transcription and the model still have to fit
	// in them.
	mediaTimeout = 30 * time.Second
)

// Media downloads an attachment. The Cloud API makes this two trips:
// the id resolves to a short-lived URL on Meta's CDN, and that URL is
// itself behind the same bearer token — a detail that is easy to miss,
// because the host is not graph.facebook.com and looks public.
type Media struct {
	client *http.Client
	base   string
	token  string
}

type MediaInput struct {
	AccessToken string

	// BaseURL overrides Meta's host, which is what a test points at.
	// The CDN URL is followed as given, so a test server can answer
	// both hops.
	BaseURL string

	Client *http.Client
}

func NewMedia(in MediaInput) (*Media, error) {
	if in.AccessToken == "" {
		return nil, errors.New("missing whatsapp access token")
	}

	base := in.BaseURL
	if base == "" {
		base = graphBaseURL
	}
	client := in.Client
	if client == nil {
		client = &http.Client{Timeout: mediaTimeout}
	}

	return &Media{client: client, base: strings.TrimSuffix(base, "/"), token: in.AccessToken}, nil
}

func MustNewMedia(in MediaInput) *Media {
	m, err := NewMedia(in)
	if err != nil {
		panic(fmt.Errorf("couldn't create the whatsapp media client: %w", err))
	}
	return m
}

// Fetch returns the bytes of one attachment and the media type Meta
// reported for it.
func (m *Media) Fetch(ctx context.Context, mediaID string) ([]byte, string, error) {
	if strings.TrimSpace(mediaID) == "" {
		return nil, "", errors.New("missing media id")
	}

	url, mediaType, size, err := m.locate(ctx, mediaID)
	if err != nil {
		return nil, "", err
	}
	if size > maxMediaBytes {
		return nil, "", fmt.Errorf("the media is %d bytes, over the %d cap", size, maxMediaBytes)
	}

	data, err := m.download(ctx, url)
	if err != nil {
		return nil, "", err
	}
	return data, mediaType, nil
}

// locate turns an id into the CDN URL, its media type and its size.
func (m *Media) locate(ctx context.Context, mediaID string) (url, mediaType string, size int64, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/%s/%s", m.base, graphVersion, mediaID), nil)
	if err != nil {
		return "", "", 0, fmt.Errorf("couldn't build the media request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+m.token)

	resp, err := m.client.Do(req)
	if err != nil {
		return "", "", 0, fmt.Errorf("couldn't reach the media api: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", "", 0, fmt.Errorf("couldn't read the media metadata: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		// A short excerpt and never the whole body: this goes to a
		// log, and the token does not belong there even by accident.
		return "", "", 0, fmt.Errorf("the media api answered %d: %s", resp.StatusCode, mediaExcerpt(raw))
	}

	var answer struct {
		URL      string `json:"url"`
		MimeType string `json:"mime_type"`
		FileSize int64  `json:"file_size"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil {
		return "", "", 0, fmt.Errorf("the media api answered something that is not json: %w", err)
	}
	if answer.URL == "" {
		return "", "", 0, errors.New("the media api answered without a url")
	}
	return answer.URL, mediaTypeOf(answer.MimeType, "audio/ogg"), answer.FileSize, nil
}

// download follows the CDN URL. The bearer goes on this request too:
// the URL is signed but not public.
func (m *Media) download(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("couldn't build the media download: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+m.token)

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("couldn't reach the media host: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Capped even though file_size was already checked: the size is
	// Meta's claim about the file, not a promise about the body.
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxMediaBytes+1))
	if err != nil {
		return nil, fmt.Errorf("couldn't read the media: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the media host answered %d: %s", resp.StatusCode, mediaExcerpt(data))
	}
	if len(data) > maxMediaBytes {
		return nil, fmt.Errorf("the media is over the %d byte cap", maxMediaBytes)
	}
	if len(data) == 0 {
		return nil, errors.New("the media host answered with nothing")
	}
	return data, nil
}

// mediaExcerpt keeps an error body short enough to log on one line.
func mediaExcerpt(raw []byte) string {
	const limit = 200
	s := strings.TrimSpace(string(raw))
	if len(s) > limit {
		return s[:limit] + "..."
	}
	return s
}
