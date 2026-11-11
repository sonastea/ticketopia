package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/persistence/dberr"
	"github.com/sonastea/ticketopia/internal/saved"
	"go.yaml.in/yaml/v3"
)

// Every problem code the API can send is published in the OpenAPI Problem
// schema, so clients can rely on the enum.
func TestProblemCodesArePublished(t *testing.T) {
	var spec struct {
		Components struct {
			Schemas struct {
				Problem struct {
					Properties struct {
						Code struct {
							Enum []string `yaml:"enum"`
						} `yaml:"code"`
					} `yaml:"properties"`
				} `yaml:"Problem"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(openAPI, &spec); err != nil {
		t.Fatal(err)
	}
	published := spec.Components.Schemas.Problem.Properties.Code.Enum
	sources, err := filepath.Glob("*.go")
	if err != nil || len(sources) == 0 {
		t.Fatalf("API sources: %v", err)
	}
	// Codes appear as accountError results, problem() assignments and
	// liveProblem(c, status, code, ...) calls.
	pattern := regexp.MustCompile(`return \d{3}, "([a-z_]+)"|code :?= "([a-z_]+)"|liveProblem\(c, \d{3}, "([a-z_]+)"`)
	found := 0
	for _, file := range sources {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range pattern.FindAllStringSubmatch(string(data), -1) {
			code := m[1] + m[2] + m[3]
			found++
			if !slices.Contains(published, code) {
				t.Errorf("%s: problem code %q is missing from the OpenAPI Problem enum", file, code)
			}
		}
	}
	if found < 20 {
		t.Fatalf("found only %d problem codes; has the pattern drifted from the source?", found)
	}
}

// A busy or unavailable database is reported as Ticketopia being busy, not as
// the event provider failing; provider cooldowns keep their own code.
func TestOverloadIsNotReportedAsProviderFailure(t *testing.T) {
	timeout := fmt.Errorf("database discover read: %w (%w)", context.DeadlineExceeded, dberr.ErrUnavailable)
	for name, tc := range map[string]struct {
		err   error
		code  string
		retry string
	}{
		"database timeout":       {timeout, "temporarily_unavailable", "5"},
		"saved snapshot timeout": {fmt.Errorf("%w: %w", saved.ErrUnavailable, timeout), "temporarily_unavailable", "5"},
		"provider cooldown":      {&discovery.UnavailableError{RetryAfter: 90 * time.Second}, "provider_unavailable", "90"},
	} {
		t.Run(name, func(t *testing.T) {
			response := httptest.NewRecorder()
			if err := problem(echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/api/v1/events", nil), response), tc.err); err != nil {
				t.Fatal(err)
			}
			var body struct{ Code, Detail string }
			_ = json.Unmarshal(response.Body.Bytes(), &body)
			if response.Code != http.StatusServiceUnavailable || body.Code != tc.code || response.Header().Get("Retry-After") != tc.retry {
				t.Fatalf("%d %q %q retry %q", response.Code, body.Code, body.Detail, response.Header().Get("Retry-After"))
			}
		})
	}
	if got := describeForAgents(timeout); !strings.Contains(got, "busy") || !strings.HasSuffix(got, "Retry after 5 seconds.") {
		t.Fatalf("agent wording: %q", got)
	}
}
