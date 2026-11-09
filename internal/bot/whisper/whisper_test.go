package whisper_test

import (
	"context"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brendalima/despensa/internal/bot/whisper"
)

const testKey = "sk-test"

// received is what the fake API saw, which is the whole contract: the
// path, the bearer, the file and the three fields.
type received struct {
	path     string
	auth     string
	filename string
	file     []byte
	fields   map[string]string
}

// serve answers one transcription and records the request.
func serve(t *testing.T, status int, body string) (*received, string) {
	t.Helper()

	got := &received{fields: map[string]string{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.path = r.URL.Path
		got.auth = r.Header.Get("Authorization")

		_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			t.Errorf("couldn't read the content type: %v", err)
		}
		reader := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Errorf("couldn't read the form: %v", err)
				break
			}
			data, _ := io.ReadAll(part)
			if part.FormName() == "file" {
				got.filename = part.FileName()
				got.file = data
				continue
			}
			got.fields[part.FormName()] = string(data)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return got, server.URL
}

func TestTheWholeRequestIsWhatTheAPIExpects(t *testing.T) {
	got, url := serve(t, http.StatusOK, `{"text":"usei uma taça de arroz"}`)

	w := whisper.MustNew(whisper.Input{APIKey: testKey, BaseURL: url})
	text, err := w.Transcribe(context.Background(), []byte("ogg bytes"), "audio/ogg; codecs=opus")
	if err != nil {
		t.Fatalf("couldn't transcribe: %v", err)
	}

	if text != "usei uma taça de arroz" {
		t.Errorf("heard %q", text)
	}
	if got.path != "/v1/audio/transcriptions" {
		t.Errorf("posted to %q", got.path)
	}
	if got.auth != "Bearer "+testKey {
		t.Errorf("authorized with %q", got.auth)
	}
	if string(got.file) != "ogg bytes" {
		t.Errorf("sent %q", got.file)
	}
	for field, want := range map[string]string{
		"model":           "whisper-1",
		"language":        "pt",
		"response_format": "json",
	} {
		if got.fields[field] != want {
			t.Errorf("%s was %q, want %q", field, got.fields[field], want)
		}
	}
}

// The model is configurable for the same reason the Anthropic one is:
// which one hears a sentence about groceries best is measured, not
// assumed.
func TestTheConfiguredModelIsTheOneAsked(t *testing.T) {
	got, url := serve(t, http.StatusOK, `{"text":"comprei pão"}`)

	w := whisper.MustNew(whisper.Input{APIKey: testKey, BaseURL: url, Model: "gpt-4o-mini-transcribe"})
	if _, err := w.Transcribe(context.Background(), []byte("x"), "audio/ogg"); err != nil {
		t.Fatalf("couldn't transcribe: %v", err)
	}

	if got.fields["model"] != "gpt-4o-mini-transcribe" {
		t.Errorf("asked for %q", got.fields["model"])
	}
}

// The API picks its decoder from the extension, and a file offered
// with none is refused as an unsupported format. An unknown type is
// called .ogg, which is what a WhatsApp voice note actually is.
func TestTheFilenameCarriesAnExtensionForEveryMediaType(t *testing.T) {
	cases := map[string]string{
		"audio/ogg; codecs=opus":   "audio.ogg",
		"":                         "audio.ogg",
		"application/octet-stream": "audio.ogg",
		"audio/mpeg":               "audio.mp3",
		"AUDIO/MP4":                "audio.m4a",
		"audio/wav":                "audio.wav",
		"audio/webm":               "audio.webm",
		"audio/flac":               "audio.flac",
	}

	for mediaType, want := range cases {
		t.Run(mediaType, func(t *testing.T) {
			got, url := serve(t, http.StatusOK, `{"text":"oi"}`)

			w := whisper.MustNew(whisper.Input{APIKey: testKey, BaseURL: url})
			if _, err := w.Transcribe(context.Background(), []byte("x"), mediaType); err != nil {
				t.Fatalf("couldn't transcribe: %v", err)
			}
			if got.filename != want {
				t.Errorf("named the file %q, want %q", got.filename, want)
			}
		})
	}
}

// Silence is an error and not an empty sentence: an empty string here
// would reach the loop as a message with nothing in it, and nobody
// would know whether the audio was blank or the call was broken.
func TestABlankTranscriptIsAnError(t *testing.T) {
	for _, body := range []string{`{"text":""}`, `{"text":"   "}`, `{}`} {
		t.Run(body, func(t *testing.T) {
			_, url := serve(t, http.StatusOK, body)

			w := whisper.MustNew(whisper.Input{APIKey: testKey, BaseURL: url})
			if _, err := w.Transcribe(context.Background(), []byte("x"), "audio/ogg"); err == nil {
				t.Fatal("a blank transcript came back as a transcript")
			}
		})
	}
}

// The error goes to a log, and an API key does not belong there even
// by accident.
func TestAFailureSaysTheStatusAndNeverTheKey(t *testing.T) {
	_, url := serve(t, http.StatusTooManyRequests, `{"error":{"message":"rate limited"}}`)

	w := whisper.MustNew(whisper.Input{APIKey: testKey, BaseURL: url})
	_, err := w.Transcribe(context.Background(), []byte("x"), "audio/ogg")
	if err == nil {
		t.Fatal("a 429 came back as a transcript")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("the error does not say what happened: %v", err)
	}
	if strings.Contains(err.Error(), testKey) {
		t.Errorf("the error leaks the key: %v", err)
	}
}

// A proxy in front of the API answers HTML, which is not a transcript
// however hard it is parsed.
func TestSomethingThatIsNotJSONIsRefused(t *testing.T) {
	_, url := serve(t, http.StatusOK, `<html>gateway</html>`)

	w := whisper.MustNew(whisper.Input{APIKey: testKey, BaseURL: url})
	if _, err := w.Transcribe(context.Background(), []byte("x"), "audio/ogg"); err == nil {
		t.Fatal("html came back as a transcript")
	}
}

func TestNothingIsSentWithoutAudioOrAKey(t *testing.T) {
	_, url := serve(t, http.StatusOK, `{"text":"oi"}`)

	w := whisper.MustNew(whisper.Input{APIKey: testKey, BaseURL: url})
	if _, err := w.Transcribe(context.Background(), nil, "audio/ogg"); err == nil {
		t.Error("an empty file was sent to the API")
	}

	if _, err := whisper.New(whisper.Input{APIKey: "  "}); err == nil {
		t.Error("a transcriber was created without a key")
	}
}
