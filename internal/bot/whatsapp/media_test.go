package whatsapp_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brendalima/despensa/internal/bot/whatsapp"
)

const mediaToken = "test-token"

// mediaServer answers both hops: the id resolves to a URL on the same
// server, and that URL serves the bytes. It records the bearer of each
// request, because the second one is the easy half to forget — the CDN
// host looks public and is not.
type mediaServer struct {
	*httptest.Server

	metadata string
	body     string
	bodyCode int

	auths []string
}

func newMediaServer(t *testing.T) *mediaServer {
	t.Helper()

	s := &mediaServer{body: "ogg bytes", bodyCode: http.StatusOK}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.auths = append(s.auths, r.Header.Get("Authorization"))

		if strings.HasPrefix(r.URL.Path, "/download") {
			w.WriteHeader(s.bodyCode)
			_, _ = io.WriteString(w, s.body)
			return
		}

		metadata := s.metadata
		if metadata == "" {
			metadata = fmt.Sprintf(
				`{"url":%q,"mime_type":"audio/ogg; codecs=opus","file_size":%d}`,
				s.URL+"/download", len(s.body))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, metadata)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *mediaServer) client(t *testing.T) *whatsapp.Media {
	t.Helper()
	return whatsapp.MustNewMedia(whatsapp.MediaInput{AccessToken: mediaToken, BaseURL: s.URL})
}

func TestTheMediaIsFetchedInTwoHopsBothAuthorized(t *testing.T) {
	server := newMediaServer(t)

	data, mediaType, err := server.client(t).Fetch(context.Background(), "media-1")
	if err != nil {
		t.Fatalf("couldn't fetch the media: %v", err)
	}

	if string(data) != "ogg bytes" {
		t.Errorf("fetched %q", data)
	}
	// The parameters are dropped here so nothing downstream has to.
	if mediaType != "audio/ogg" {
		t.Errorf("the media type came back as %q", mediaType)
	}
	if len(server.auths) != 2 {
		t.Fatalf("made %d requests, want the metadata and the download", len(server.auths))
	}
	for i, auth := range server.auths {
		if auth != "Bearer "+mediaToken {
			t.Errorf("request %d authorized with %q", i+1, auth)
		}
	}
}

// Meta's own ceiling for audio is the read cap. Anything above it is
// not a sentence about groceries, and it is refused before a byte of
// it is downloaded.
func TestMediaOverTheCapIsRefusedBeforeTheDownload(t *testing.T) {
	server := newMediaServer(t)
	server.metadata = fmt.Sprintf(`{"url":%q,"mime_type":"audio/ogg","file_size":%d}`,
		server.URL+"/download", (16<<20)+1)

	if _, _, err := server.client(t).Fetch(context.Background(), "media-1"); err == nil {
		t.Fatal("an oversized file was fetched")
	}
	if len(server.auths) != 1 {
		t.Errorf("made %d requests, want only the metadata one", len(server.auths))
	}
}

func TestAMediaFailureSaysTheStatusAndNeverTheToken(t *testing.T) {
	cases := map[string]func(*mediaServer){
		"metadata": func(s *mediaServer) { s.metadata = `` },
		"download": func(s *mediaServer) { s.bodyCode = http.StatusForbidden; s.body = "denied" },
	}

	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			server := newMediaServer(t)
			if name == "metadata" {
				// A metadata answer with no url is as useless as a 404,
				// and far more likely: the id expired.
				server.metadata = `{"mime_type":"audio/ogg"}`
			} else {
				setup(server)
			}

			_, _, err := server.client(t).Fetch(context.Background(), "media-1")
			if err == nil {
				t.Fatal("a broken fetch came back as bytes")
			}
			if strings.Contains(err.Error(), mediaToken) {
				t.Errorf("the error leaks the token: %v", err)
			}
		})
	}
}

func TestNothingIsFetchedWithoutAnIDOrAToken(t *testing.T) {
	server := newMediaServer(t)

	if _, _, err := server.client(t).Fetch(context.Background(), "  "); err == nil {
		t.Error("a blank media id was sent to the API")
	}
	if _, err := whatsapp.NewMedia(whatsapp.MediaInput{}); err == nil {
		t.Error("a media client was created without a token")
	}
}
