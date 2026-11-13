package oauth

import (
	"net/http"
	"time"
)

// SetNow fixes the service clock for tests.
func SetNow(s *Service, now func() time.Time) { s.now = now }

// SetDocumentsNow fixes the document cache clock for tests.
func SetDocumentsNow(d *Documents, now func() time.Time) { d.now = now }

// DocumentTTL exposes cache lifetime parsing.
var DocumentTTL = documentTTL

// HTTPClient is the client documents are fetched with.
func HTTPClient(d *Documents) *http.Client { return d.client }
