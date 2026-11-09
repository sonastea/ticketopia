package reviewfixture

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// DefaultSESEndpoint is MiniStack's loopback endpoint from make ses-setup.
const DefaultSESEndpoint = "http://localhost:4566"

// sesAccount is the account MiniStack files captures under for the dummy
// credentials local SES mode uses.
const sesAccount = "000000000000"

// Message is one email captured by MiniStack. It is private development data.
type Message struct {
	MessageID string    `json:"message_id"`
	To        []string  `json:"to"`
	Subject   string    `json:"subject"`
	BodyText  string    `json:"body_text"`
	SentAt    time.Time `json:"sent_at"`
}

// sesEndpoint validates a local SES emulator origin, as the reminder sender
// itself requires, and returns it without a trailing slash.
func sesEndpoint(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || (u.Path != "" && u.Path != "/") ||
		u.RawQuery != "" || u.ForceQuery || u.Fragment != "" ||
		!slices.Contains([]string{"localhost", "127.0.0.1", "::1"}, u.Hostname()) {
		return "", fmt.Errorf("SES endpoint must be a loopback HTTP(S) origin such as %s", DefaultSESEndpoint)
	}
	return strings.TrimSuffix(raw, "/"), nil
}

// Messages returns the emails MiniStack captured for this run's fixture
// accounts, oldest first. Account addresses embed the run token, so captures
// from other runs or manual testing are excluded.
func Messages(ctx context.Context, endpoint, run string) ([]Message, error) {
	endpoint, err := sesEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/_ministack/ses/messages?account="+sesAccount, nil)
	if err != nil {
		return nil, err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("reading SES captures: is MiniStack running (make ses-setup)? %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("reading SES captures: status %d", response.StatusCode)
	}
	var captures struct {
		Messages map[string][]struct {
			MessageID string   `json:"MessageId"`
			To        []string `json:"To"`
			Subject   string   `json:"Subject"`
			BodyText  string   `json:"BodyText"`
			Timestamp float64  `json:"Timestamp"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(response.Body).Decode(&captures); err != nil {
		return nil, fmt.Errorf("reading SES captures: %w", err)
	}
	marker := "+" + run + "@"
	messages := []Message{}
	for _, c := range captures.Messages[sesAccount] {
		if !slices.ContainsFunc(c.To, func(to string) bool { return strings.Contains(to, marker) }) {
			continue
		}
		messages = append(messages, Message{MessageID: c.MessageID, To: c.To, Subject: c.Subject, BodyText: c.BodyText,
			SentAt: time.Unix(0, int64(c.Timestamp*float64(time.Second))).UTC()})
	}
	slices.SortStableFunc(messages, func(a, b Message) int { return a.SentAt.Compare(b.SentAt) })
	return messages, nil
}
