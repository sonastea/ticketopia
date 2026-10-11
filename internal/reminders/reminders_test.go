package reminders

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/smithy-go"
	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/saved"
)

func TestScheduleAndLead(t *testing.T) {
	now := time.Date(2026, 10, 9, 9, 0, 0, 0, time.UTC)
	sale := now.Add(time.Hour)
	for _, tc := range []struct {
		name, status string
		tbd          bool
		start        *time.Time
		want         string
	}{
		{"known", "onsale", false, &sale, "scheduled"}, {"unknown", "onsale", false, nil, "pending_date"}, {"tbd", "onsale", true, &sale, "pending_date"}, {"cancelled", "cancelled", false, &sale, "cancelled"}, {"postponed", "postponed", false, &sale, "cancelled"}, {"already", "onsale", false, &now, "expired"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, at, due := Schedule(models.Event{Status: tc.status, PublicSale: models.PublicSale{Start: tc.start, StartTBD: tc.tbd}}, 30, now)
			if state != tc.want {
				t.Fatal(state)
			}
			if state == "scheduled" && (!at.Equal(sale) || !due.Equal(now.Add(30*time.Minute))) {
				t.Fatal(at, due)
			}
		})
	}
	for _, n := range []int{-1, 0, 10081} {
		if Lead(n) == nil {
			t.Fatal("accepted invalid lead", n)
		}
	}
	for _, n := range []int{1, 30, 10080} {
		if Lead(n) != nil {
			t.Fatal(n)
		}
	}
}

type listRepo struct {
	Repository
	items []Item
}

func (r *listRepo) List(_ context.Context, _ string, q saved.Query) ([]Item, error) {
	if q.Cursor != nil {
		return r.items[1:], nil
	}
	return r.items, nil
}
func TestReminderPaginationIsOwnerAndCollectionBound(t *testing.T) {
	owner := "00000000000000000000000000000001"
	at := time.Now().UTC()
	r := &listRepo{items: []Item{{EventID: "ticketmaster:first", CreatedAt: at}, {EventID: "ticketmaster:second", CreatedAt: at.Add(-time.Second)}}}
	s := New(r)
	page, err := s.List(t.Context(), owner, url.Values{"limit": {"1"}})
	if err != nil || len(page.Items) != 1 || page.NextCursor == nil {
		t.Fatal(page, err)
	}
	if _, err = s.List(t.Context(), owner, url.Values{"limit": {"1"}, "cursor": {*page.NextCursor}}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.List(t.Context(), "00000000000000000000000000000002", url.Values{"cursor": {*page.NextCursor}}); err == nil {
		t.Fatal("foreign cursor accepted")
	}
	bookmark := saved.EncodeCursor(owner, at, "ticketmaster:first")
	if _, err = s.List(t.Context(), owner, url.Values{"cursor": {*bookmark}}); err == nil {
		t.Fatal("saved cursor accepted as reminder cursor")
	}
	if _, err = saved.ParseQuery(url.Values{"cursor": {*page.NextCursor}}, owner); err == nil {
		t.Fatal("reminder cursor accepted as bookmark cursor")
	}
}

type fakeSES struct {
	input *sesv2.SendEmailInput
	err   error
	calls int
}

func (f *fakeSES) SendEmail(_ context.Context, in *sesv2.SendEmailInput, _ ...func(*sesv2.Options)) (*sesv2.SendEmailOutput, error) {
	f.calls++
	f.input = in
	return &sesv2.SendEmailOutput{MessageId: aws.String("receipt")}, f.err
}
func TestSESOutcomesAndPrivateCopy(t *testing.T) {
	for _, tc := range []struct{ code, want string }{
		{"", "accepted"}, {"TooManyRequestsException", "retry"}, {"LimitExceededException", "retry"}, {"MessageRejected", "failed"}, {"AccessDeniedException", "failed"}, {"InternalServiceError", "uncertain"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			f := &fakeSES{}
			if tc.code != "" {
				f.err = &smithy.GenericAPIError{Code: tc.code, Message: "private recipient canary"}
			}
			s := &SES{f, Config{From: "notify@example.com", BaseURL: "https://events.example", ConfigurationSet: "reminders"}}
			state, id := s.Send(t.Context(), Delivery{To: "private@example.com", Name: "A\r\nshow", URL: "https://ticketmaster.com/event/test", Timezone: "America/New_York", SaleAt: time.Date(2026, 11, 1, 15, 0, 0, 0, time.UTC), UnsubscribeToken: "secret"})
			if state != tc.want || f.calls != 1 {
				t.Fatal(state, f.calls)
			}
			if state == "accepted" && id != "receipt" {
				t.Fatal(id)
			}
			if len(f.input.Destination.ToAddresses) != 1 || strings.Contains(aws.ToString(f.input.Content.Simple.Subject.Data), "\n") {
				t.Fatal("unsafe recipient or subject")
			}
			body := aws.ToString(f.input.Content.Simple.Body.Text.Data)
			if !strings.Contains(body, "10:00 EST") || !strings.Contains(body, "/reminders/unsubscribe/secret") {
				t.Fatal(body)
			}
		})
	}
	f := &fakeSES{err: errors.New("connection lost")}
	s := &SES{client: f}
	state, _ := s.Send(t.Context(), Delivery{})
	if state != "uncertain" {
		t.Fatal(state)
	}
}
func TestConfigOptIn(t *testing.T) {
	t.Setenv("SES_ENDPOINT_URL", "")
	t.Setenv("REMINDERS_ENABLED", "")
	t.Setenv("AWS_REGION", "")
	t.Setenv("REMINDER_SEND_INTERVAL", "")
	if c, err := ConfigFromEnv(); err != nil || c.Enabled || c.Region != "us-east-1" {
		t.Fatal(c, err)
	}
	t.Setenv("REMINDERS_ENABLED", "true")
	t.Setenv("REMINDERS_EMAIL_FROM", "Ticketopia <notify@example.com>")
	t.Setenv("AUTH_BASE_URL", "https://events.example")
	if c, err := ConfigFromEnv(); err != nil || c.Region != "us-east-1" {
		t.Fatal(c, err)
	}
	t.Setenv("AWS_REGION", "us-east-2")
	if c, err := ConfigFromEnv(); err != nil || c.Region != "us-east-2" {
		t.Fatal(c, err)
	}
	t.Setenv("AWS_REGION", "invalid-region")
	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("invalid region accepted")
	}
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("REMINDER_SEND_INTERVAL", "10ms")
	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("unbounded sending")
	}
}

type workerRepo struct {
	Repository
	claim    Claim
	prepared bool
	fresh    *models.EventDetail
	finished string
}

func (r *workerRepo) Claim(context.Context) (Claim, error) { return r.claim, nil }
func (r *workerRepo) Prepare(_ context.Context, _ Claim, d *models.EventDetail) (*Delivery, error) {
	r.prepared = true
	r.fresh = d
	if d == nil {
		return nil, nil
	}
	return &Delivery{JobID: "job"}, nil
}
func (r *workerRepo) Finish(_ context.Context, _ string, state, _ string) error {
	r.finished = state
	return nil
}

type workerProvider struct {
	detail models.EventDetail
	err    error
	calls  int
}

func (p *workerProvider) RefreshEvent(context.Context, string) (models.EventDetail, error) {
	p.calls++
	return p.detail, p.err
}

type workerSender struct{}

func (workerSender) Send(context.Context, Delivery) (string, string) { return "uncertain", "" }
func TestWorkerRequiresFreshDataAndRecordsUncertainty(t *testing.T) {
	detail := models.EventDetail{Item: models.Event{ID: "ticketmaster:test"}, Meta: models.Freshness{DataAsOf: time.Now()}}
	r := &workerRepo{claim: Claim{ID: "setting", EventID: detail.Item.ID}}
	p := &workerProvider{err: errors.New("upstream unavailable")}
	w := NewWorker(r, p, workerSender{}, zerolog.Nop(), time.Second)
	if err := w.Step(t.Context()); err != nil || !r.prepared || r.fresh != nil || r.finished != "" {
		t.Fatal("sent stale data", err)
	}
	p.err = nil
	p.detail = detail
	if err := w.Step(t.Context()); err != nil || r.finished != "uncertain" {
		t.Fatal(err, r.finished)
	}
	r.claim.Paused = true
	p.calls = 0
	if err := w.Step(t.Context()); err != nil || p.calls != 0 {
		t.Fatal("paused reminder consumed provider budget", err)
	}
}
