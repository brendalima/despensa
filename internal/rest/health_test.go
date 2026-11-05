package rest_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/brendalima/despensa/internal/rest"
)

type fakePinger struct {
	err    error
	pinged int
}

func (p *fakePinger) Ping(context.Context) error {
	p.pinged++
	return p.err
}

// The platform's health check uses /healthz, and a sleeping database
// must not restart a healthy machine.
func TestLiveTouchesNothing(t *testing.T) {
	db := &fakePinger{err: errors.New("database is asleep")}
	handler := rest.MustNewHealth(db)

	res := httptest.NewRecorder()
	handler.Live(res, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if res.Code != http.StatusOK {
		t.Errorf("status %d, want 200", res.Code)
	}
	if db.pinged != 0 {
		t.Errorf("/healthz pinged the database %d times", db.pinged)
	}
}

func TestReadyAnswersForTheDatabase(t *testing.T) {
	cases := map[string]struct {
		err  error
		want int
	}{
		"reachable":   {nil, http.StatusOK},
		"unreachable": {errors.New("no connection"), http.StatusServiceUnavailable},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			db := &fakePinger{err: c.err}
			handler := rest.MustNewHealth(db)

			res := httptest.NewRecorder()
			handler.Ready(res, httptest.NewRequest(http.MethodGet, "/readyz", nil))

			if res.Code != c.want {
				t.Errorf("status %d, want %d", res.Code, c.want)
			}
			if db.pinged != 1 {
				t.Errorf("/readyz pinged the database %d times, want 1", db.pinged)
			}
		})
	}
}

func TestHealthNeedsTheDatabase(t *testing.T) {
	if _, err := rest.NewHealth(nil); err == nil {
		t.Error("NewHealth accepted a missing database")
	}
}
