// Package rest is the HTTP border: it reads a request, checks what has
// to be checked before anything else happens, and hands the work on. No
// rule of the pantry lives here.
package rest

import "context"

// Pinger is the one thing the readiness check needs. Declared here so
// this package depends on a shape it owns; repository.Health satisfies
// it without knowing it exists.
type Pinger interface {
	Ping(ctx context.Context) error
}
