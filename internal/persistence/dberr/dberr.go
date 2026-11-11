// Package dberr holds the database error that callers outside persistence,
// such as the HTTP API, recognize without importing persistence itself.
package dberr

import "errors"

// ErrUnavailable marks a database that is down, refused the operation, or did
// not answer in time (for example every pooled connection was busy).
var ErrUnavailable = errors.New("database unavailable")
