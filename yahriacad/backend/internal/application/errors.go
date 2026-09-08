// Package application hosts the shared use-case error sentinels.
//
// The domain contract (docs/architecture/contracts.md §4) states that
// repositories must return a "not found" error matched with errors.Is, but
// the frozen domain layer does not define the variable. These sentinels are
// therefore the canonical values returned by every persistence adapter and
// mapped onto HTTP status codes by the REST layer.
package application

import "errors"

// ErrNotFound is returned by Repository implementations when the requested
// project does not exist. Use cases wrap it with %w and the REST layer maps
// it onto 404 {"error":{"code":"not_found"}}.
var ErrNotFound = errors.New("projet : introuvable")

// ErrDuplicate is returned when a Create would insert an identifier that
// already exists. The REST layer maps it onto 409 conflict.
var ErrDuplicate = errors.New("projet : identifiant déjà utilisé")
