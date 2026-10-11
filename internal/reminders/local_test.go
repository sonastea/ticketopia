package reminders

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"go.yaml.in/yaml/v3"
)

func TestLocalSESConfigurationBoundaries(t *testing.T) {
	t.Setenv("REMINDERS_ENABLED", "true")
	t.Setenv("AWS_REGION", "")
	t.Setenv("REMINDERS_EMAIL_FROM", "Ticketopia DEV <notifications-dev@ticketopia.sonastea.com>")
	t.Setenv("SES_CONFIGURATION_SET", "")
	t.Setenv("REMINDER_SEND_INTERVAL", "")
	for _, tc := range []struct {
		endpoint, base string
		valid          bool
	}{
		{"", "https://events.example", true},
		{"", "http://localhost:8080", false},
		{"http://localhost:4566", "http://localhost:8080", true},
		{"http://127.0.0.1:4566/", "http://127.0.0.1:8080", true},
		{"http://[::1]:4566", "http://[::1]:8080", true},
		{"http://localhost:4566", "http://events.example", false},
		{"http://localhost:4566", "http://localhost.evil.example", false},
		{"https://email.us-east-1.amazonaws.com", "https://events.example", false},
		{"http://localhost.evil.example:4566", "http://localhost:8080", false},
		{"http://user:password@localhost:4566", "http://localhost:8080", false},
		{"http://localhost:4566/path", "http://localhost:8080", false},
		{"http://localhost:4566?", "http://localhost:8080", false},
		{"http://localhost:4566?x=y", "http://localhost:8080", false},
		{"http://localhost:4566#fragment", "http://localhost:8080", false},
		{"ftp://localhost:4566", "http://localhost:8080", false},
	} {
		t.Run(tc.endpoint+"/"+tc.base, func(t *testing.T) {
			t.Setenv("SES_ENDPOINT_URL", tc.endpoint)
			t.Setenv("AUTH_BASE_URL", tc.base)
			c, err := ConfigFromEnv()
			if (err == nil) != tc.valid {
				t.Fatal(c, err)
			}
		})
	}
	t.Setenv("SES_ENDPOINT_URL", "http://localhost:4566")
	t.Setenv("AUTH_BASE_URL", "http://localhost:8080")
	t.Setenv("SES_CONFIGURATION_SET", "production-set")
	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("local mode inherited production configuration set")
	}
}

func TestLocalSESSDKUsesDummyCredentialsAndExplicitEndpoint(t *testing.T) {
	requests := make(chan *http.Request, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/email/outbound-emails" || r.Method != "POST" {
			t.Error("unexpected SESv2 request", r.Method, r.URL.Path)
		}
		var input sesv2.SendEmailInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
		}
		if input.Destination == nil || len(input.Destination.ToAddresses) != 1 || input.Destination.ToAddresses[0] != "recipient@example.com" {
			t.Error("recipient changed", input.Destination)
		}
		requests <- r
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"MessageId":"local-receipt"}`))
	}))
	defer server.Close()
	t.Setenv("AWS_ACCESS_KEY_ID", "production-access-canary")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "production-secret-canary")
	t.Setenv("AWS_SESSION_TOKEN", "production-session-canary")
	t.Setenv("AWS_PROFILE", "absent-production-profile-canary")
	t.Setenv("AWS_ENDPOINT_URL", "https://must-not-contact.example")
	t.Setenv("AWS_ENDPOINT_URL_SESV2", "https://must-not-contact.example")
	t.Setenv("AWS_IGNORE_CONFIGURED_ENDPOINT_URLS", "false")
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(t.TempDir(), "absent"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "absent"))
	c := Config{Region: "us-east-1", From: "notifications-dev@ticketopia.sonastea.com", BaseURL: "http://localhost:8080", EndpointURL: server.URL}
	s, err := NewSES(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	if s.client.(*sesv2.Client).Options().RetryMaxAttempts != 1 {
		t.Fatal("SDK retries could duplicate local or real sends")
	}
	state, id := s.Send(t.Context(), Delivery{To: "recipient@example.com", Name: "Local event", SaleAt: time.Now().Add(time.Hour), Timezone: "UTC", UnsubscribeToken: "local-token"})
	if state != "accepted" || id != "local-receipt" {
		t.Fatal(state, id)
	}
	r := <-requests
	if !strings.Contains(r.Header.Get("Authorization"), "Credential=test/") || strings.Contains(r.Header.Get("Authorization"), "canary") || r.Header.Get("X-Amz-Security-Token") != "" {
		t.Fatal("emulator received real credentials")
	}
	// Never silently fall back to real SES when a caller expected a mock endpoint.
	t.Setenv("AWS_PROFILE", "")
	c.EndpointURL = ""
	c.BaseURL = "https://events.example"
	s, err = NewSES(t.Context(), c)
	if err == nil || s != nil {
		t.Fatal("implicit SDK endpoint did not stop startup")
	}
	t.Setenv("AWS_ENDPOINT_URL", "")
	t.Setenv("AWS_ENDPOINT_URL_SESV2", "")
	s, err = NewSES(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	if s.client.(*sesv2.Client).Options().BaseEndpoint != nil {
		t.Fatal("implicit endpoint bypassed emulator safety boundary")
	}
	if s.client.(*sesv2.Client).Options().RetryMaxAttempts != 1 {
		t.Fatal("production SDK retries enabled")
	}
}

func TestLocalSESSDKDoesNotRetryAmbiguousFailures(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"message":"synthetic ambiguous acceptance"}`))
	}))
	defer server.Close()
	s, err := NewSES(t.Context(), Config{Region: "us-east-1", EndpointURL: server.URL, From: "notifications-dev@ticketopia.sonastea.com", BaseURL: "http://localhost:8080"})
	if err != nil {
		t.Fatal(err)
	}
	state, _ := s.Send(t.Context(), Delivery{To: "fixture@example.com", Name: "No duplicate retry", SaleAt: time.Now().Add(time.Hour), Timezone: "UTC"})
	if state != "uncertain" || calls.Load() != 1 {
		t.Fatal("ambiguous send was retried", state, calls.Load())
	}
}

func TestLocalSESSetupWorkflow(t *testing.T) {
	makePath, err := exec.LookPath("make")
	if err != nil {
		t.Skip("make is required for workflow tests")
	}
	steps := []string{"ses-start", "ses-get", "ses-create", "ses-check", "database"}
	for _, tc := range []struct {
		name, failure      string
		existing, combined bool
	}{
		{"new identity without token", "", false, false}, {"existing identity", "", true, false},
		{"combined setup without token", "", false, true},
		{"startup failure", "ses-start", false, true}, {"identity failure", "ses-create", false, true},
		{"verification failure", "ses-check", false, true}, {"database failure", "database", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "local project")
			write := func(name, body string, mode os.FileMode) {
				t.Helper()
				p := filepath.Join(root, name)
				if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte(body), mode); err != nil {
					t.Fatal(err)
				}
			}
			for _, name := range []string{"Makefile", "scripts/ses-setup.sh", "deploy/ministack/compose.yaml"} {
				data, err := os.ReadFile(filepath.Join("../..", name))
				if err != nil {
					t.Fatal(err)
				}
				write(name, string(data), 0644)
			}
			write(".env", "LOCALSTACK_AUTH_TOKEN=must-not-load-env-canary\nAWS_ACCESS_KEY_ID=production-canary\nSMTP_HOST=production-relay-canary\n", 0600)
			write("scripts/mariadb-setup.sh", `#!/bin/sh
printf '%s\n' database >> "$WORKFLOW_LOG"
if [ "$FAIL_STEP" = database ]; then exit 9; fi
`, 0644)
			write("tools/docker", `#!/bin/sh
set -eu
case "$*" in
  "compose --env-file /dev/null -f "*) ;;
  *) echo 'unsafe compose invocation' >&2; exit 1 ;;
esac
case "$*" in
  *"up -d --wait --wait-timeout 180 ministack") step=ses-start ;;
  *"exec -T "*)
    case "$*" in
      *"-e AWS_ACCESS_KEY_ID=test -e AWS_SECRET_ACCESS_KEY=test -e AWS_SESSION_TOKEN= "*"ministack awslocal --endpoint-url http://localhost:4566 --region us-east-1 "*) ;;
      *) echo 'unsafe AWS invocation' >&2; exit 1 ;;
    esac
    case "$*" in
      *"get-email-identity"*"--query VerifiedForSendingStatus --output text") step=ses-check ;;
      *"get-email-identity"*) step=ses-get ;;
      *"create-email-identity"*) step=ses-create ;;
      *) exit 1 ;;
    esac ;;
  *) exit 1 ;;
esac
printf '%s\n' "$step" >> "$WORKFLOW_LOG"
if [ "$FAIL_STEP" = "$step" ]; then exit 9; fi
if [ "$step" = ses-get ] && [ "$EXISTING_IDENTITY" != true ]; then exit 1; fi
if [ "$step" = ses-check ]; then echo True; fi
`, 0755)
			target := "ses-setup"
			if tc.combined {
				target = "local-setup"
			}
			command := exec.Command(makePath, "-j4", target)
			command.Dir = root
			existing := "false"
			if tc.existing {
				existing = "true"
			}
			log := filepath.Join(root, "workflow.log")
			command.Env = append(os.Environ(), "PATH="+filepath.Join(root, "tools")+string(os.PathListSeparator)+os.Getenv("PATH"), "WORKFLOW_LOG="+log, "FAIL_STEP="+tc.failure, "EXISTING_IDENTITY="+existing, "LOCALSTACK_AUTH_TOKEN=", "AWS_ENDPOINT_URL=https://production.invalid", "AWS_ACCESS_KEY_ID=production-canary", "AWS_SECRET_ACCESS_KEY=production-secret-canary", "REMINDERS_EMAIL_FROM=production@production.invalid", "SMTP_HOST=production-relay-canary")
			output, err := command.CombinedOutput()
			if (err != nil) != (tc.failure != "") {
				t.Fatalf("workflow: %v\n%s", err, output)
			}
			data, readErr := os.ReadFile(log)
			if readErr != nil {
				t.Fatal(readErr)
			}
			want := []string{}
			for _, step := range steps {
				if step == "ses-create" && tc.existing {
					continue
				}
				if step == "database" && !tc.combined {
					continue
				}
				want = append(want, step)
				if step == tc.failure {
					break
				}
			}
			if string(data) != strings.Join(want, "\n")+"\n" {
				t.Fatalf("wrong step order: %q", data)
			}
			if strings.Contains(string(output), "canary") {
				t.Fatal("setup logged credentials")
			}
			if strings.Contains(string(output), "Local SES sender is ready") != (tc.failure == "" || tc.failure == "database") {
				t.Fatal("false SES readiness", string(output))
			}
		})
	}
}

func TestLocalSESComposeIsolation(t *testing.T) {
	data, err := os.ReadFile("../../deploy/ministack/compose.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var compose struct {
		Name     string `yaml:"name"`
		Services map[string]struct {
			Image       string            `yaml:"image"`
			Ports       []string          `yaml:"ports"`
			Environment map[string]string `yaml:"environment"`
			Volumes     []string          `yaml:"volumes"`
			Healthcheck struct {
				Test []string `yaml:"test"`
			} `yaml:"healthcheck"`
		} `yaml:"services"`
	}
	if err = yaml.Unmarshal(data, &compose); err != nil {
		t.Fatal(err)
	}
	c, ok := compose.Services["ministack"]
	if !ok || len(compose.Services) != 1 || compose.Name != "ticketopia-ses" || c.Image != "ministackorg/ministack:1.5.24" {
		t.Fatal("unexpected local SES service")
	}
	if len(c.Ports) != 1 || c.Ports[0] != "127.0.0.1:4566:4566" || len(c.Volumes) != 0 {
		t.Fatal("emulator exposed publicly or gained host mounts")
	}
	if relay, ok := c.Environment["SMTP_HOST"]; !ok || relay != "" || c.Environment["PERSIST_STATE"] != "0" {
		t.Fatal("unsafe SES emulator defaults")
	}
	for name := range c.Environment {
		if name != "SMTP_HOST" && name != "PERSIST_STATE" {
			t.Fatal("unexpected emulator environment (relay/credential isolation)", name)
		}
	}
	if !strings.Contains(strings.Join(c.Healthcheck.Test, " "), "--endpoint-url http://localhost:4566 --region us-east-1 sesv2 list-email-identities") {
		t.Fatal("health check did not check SES readiness")
	}
}

func TestMiniStackSESSendAndCapture(t *testing.T) {
	endpoint := os.Getenv("SES_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("set SES_TEST_ENDPOINT after make ses-setup for real emulator verification")
	}
	u, err := url.Parse(endpoint)
	if err != nil || !origin(u) || !loopback(u) || (u.Scheme != "http" && u.Scheme != "https") {
		t.Fatal("SES_TEST_ENDPOINT must be a loopback HTTP(S) origin")
	}
	c := Config{Region: "us-east-1", From: "Ticketopia DEV <notifications-dev@ticketopia.sonastea.com>", BaseURL: "http://localhost:8080", EndpointURL: strings.TrimSuffix(endpoint, "/")}
	s, err := NewSES(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	name := "Synthetic local SES smoke test"
	state, id := s.Send(t.Context(), Delivery{To: "local-fixture@example.com", Name: name, URL: "https://example.com/tickets", Timezone: "UTC", SaleAt: time.Now().Add(time.Hour), UnsubscribeToken: "synthetic-local-token"})
	if state != "accepted" || id == "" {
		t.Fatal("local SES did not accept the synthetic message", state)
	}
	req, err := http.NewRequestWithContext(t.Context(), "GET", c.EndpointURL+"/_ministack/ses/messages?account=000000000000", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("capture read failed", response.StatusCode)
	}
	var capture struct {
		Messages map[string][]struct {
			ID       string   `json:"MessageId"`
			Subject  string   `json:"Subject"`
			To       []string `json:"To"`
			BodyText string   `json:"BodyText"`
		} `json:"messages"`
	}
	if err = json.NewDecoder(response.Body).Decode(&capture); err != nil {
		t.Fatal(err)
	}
	for _, message := range capture.Messages["000000000000"] {
		if message.ID == id && strings.Contains(message.Subject, name) && len(message.To) == 1 && message.To[0] == "local-fixture@example.com" && strings.Contains(message.BodyText, "http://localhost:8080/reminders/unsubscribe/synthetic-local-token") {
			return
		}
	}
	t.Fatal("accepted message was not found in MiniStack captures")
}
