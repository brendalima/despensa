// Package whisper is the transcriber: it turns a voice note into the
// sentence the model reads. It is the only package that talks to
// OpenAI, and it knows nothing about WhatsApp or the pantry — audio
// bytes and a media type in, words out.
package whisper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/brendalima/despensa/internal/bot"
)

var _ bot.Transcriber = (*Whisper)(nil)

// Whisper is a transcription step of its own and not part of the model
// call, because the Messages API takes text, images and documents and
// no audio in any model.
//
// It is plain net/http rather than a second vendor SDK: one multipart
// POST does not earn a dependency, and a test can point BaseURL at an
// httptest server and exercise the whole mapping without a key.
type Whisper struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

// Input is the transcriber's settings.
type Input struct {
	APIKey string

	// Model is the transcription model. Empty means whisper-1. It is
	// open for the same reason ANTHROPIC_MODEL is: what matters is how
	// well it hears a sentence about groceries in Brazilian
	// Portuguese, and that is measured, not assumed.
	Model string

	// BaseURL overrides the API host. It exists for the tests, which
	// point it at an httptest server; empty means the real API.
	BaseURL string

	// Client exists for the same reason. Nil means a client with a
	// timeout, because a transcription that never returns would hold
	// the whole message past the two minutes rest.WhatsApp gives it.
	Client *http.Client
}

// defaultModel is the one this was priced against.
const defaultModel = "whisper-1"

// transcribeTimeout bounds one call. A sentence about what was used or
// bought is seconds of audio, and the transcription is only the first
// of three or four round trips that have to fit inside the webhook's
// two-minute work budget.
const transcribeTimeout = 60 * time.Second

func New(in Input) (*Whisper, error) {
	if strings.TrimSpace(in.APIKey) == "" {
		return nil, errors.New("missing openai api key")
	}

	model := strings.TrimSpace(in.Model)
	if model == "" {
		model = defaultModel
	}
	baseURL := strings.TrimSuffix(in.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	client := in.Client
	if client == nil {
		client = &http.Client{Timeout: transcribeTimeout}
	}

	return &Whisper{apiKey: in.APIKey, model: model, baseURL: baseURL, client: client}, nil
}

func MustNew(in Input) *Whisper {
	w, err := New(in)
	if err != nil {
		panic(fmt.Errorf("couldn't create the transcriber: %w", err))
	}
	return w
}

// Transcribe sends one audio file and returns what was said.
//
// The language is pinned to Portuguese rather than detected. Detection
// on a few seconds of speech is a coin toss that occasionally comes
// back as Spanish, and a sentence transcribed into the wrong language
// reads to the model as nonsense — which, with tools in its hand,
// means a wrong write and not a shrug.
func (w *Whisper) Transcribe(ctx context.Context, audio []byte, mediaType string) (string, error) {
	if len(audio) == 0 {
		return "", errors.New("no audio to transcribe")
	}

	body := &bytes.Buffer{}
	form := multipart.NewWriter(body)

	// The filename is not decoration: the API picks its decoder from
	// the extension, and a voice note offered with no extension is
	// refused as an unsupported format.
	file, err := form.CreateFormFile("file", "audio"+audioExtension(mediaType))
	if err != nil {
		return "", fmt.Errorf("couldn't build the transcription request: %w", err)
	}
	if _, err := file.Write(audio); err != nil {
		return "", fmt.Errorf("couldn't build the transcription request: %w", err)
	}
	for _, field := range [][2]string{
		{"model", w.model},
		{"language", "pt"},
		{"response_format", "json"},
	} {
		if err := form.WriteField(field[0], field[1]); err != nil {
			return "", fmt.Errorf("couldn't build the transcription request: %w", err)
		}
	}
	if err := form.Close(); err != nil {
		return "", fmt.Errorf("couldn't build the transcription request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		w.baseURL+"/v1/audio/transcriptions", body)
	if err != nil {
		return "", fmt.Errorf("couldn't build the transcription request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+w.apiKey)
	req.Header.Set("Content-Type", form.FormDataContentType())

	resp, err := w.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("couldn't reach the transcriber: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// The body is read under a cap. A proxy answering an error page
	// instead of the API would otherwise be read into memory whole.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("couldn't read the transcription: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		// The status and a short excerpt, never the whole body: the
		// error goes to a log, and an API key does not belong there
		// even by accident.
		return "", fmt.Errorf("the transcriber answered %d: %s", resp.StatusCode, excerpt(raw))
	}

	var answer struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil {
		return "", fmt.Errorf("the transcriber answered something that is not json: %w", err)
	}

	// Silence is an error and not an empty sentence. An empty string
	// here would reach the loop as a message with nothing in it, and
	// the person would be told to send text without anyone knowing
	// whether the audio was blank or the call was broken.
	text := strings.TrimSpace(answer.Text)
	if text == "" {
		return "", errors.New("the transcriber heard nothing")
	}
	return text, nil
}

// audioExtension names the file after the media type WhatsApp
// reported. An unknown type gets .ogg, which is what a WhatsApp voice
// note actually is: guessing right is better than offering no
// extension at all, which is refused outright.
func audioExtension(mediaType string) string {
	base, _, _ := strings.Cut(mediaType, ";")
	switch strings.ToLower(strings.TrimSpace(base)) {
	case "audio/mpeg", "audio/mp3":
		return ".mp3"
	case "audio/mp4", "audio/m4a", "audio/x-m4a":
		return ".m4a"
	case "audio/wav", "audio/x-wav":
		return ".wav"
	case "audio/webm":
		return ".webm"
	case "audio/flac":
		return ".flac"
	default:
		return ".ogg"
	}
}

// excerpt keeps an error body short enough to log on one line.
func excerpt(raw []byte) string {
	const limit = 200
	s := strings.TrimSpace(string(raw))
	if len(s) > limit {
		return s[:limit] + "..."
	}
	return s
}
