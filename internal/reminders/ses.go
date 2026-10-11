package reminders

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/aws-sdk-go-v2/service/sesv2/types"
	"github.com/aws/smithy-go"
)

type Config struct {
	Enabled                                 bool
	Region, From, BaseURL, ConfigurationSet string
	Interval                                time.Duration
	EndpointURL                             string
}

func ConfigFromEnv() (Config, error) {
	c := Config{Region: os.Getenv("AWS_REGION"), From: os.Getenv("REMINDERS_EMAIL_FROM"), BaseURL: os.Getenv("AUTH_BASE_URL"), ConfigurationSet: os.Getenv("SES_CONFIGURATION_SET"), Interval: time.Second, EndpointURL: os.Getenv("SES_ENDPOINT_URL")}
	if c.Region == "" {
		c.Region = "us-east-1"
	}
	if raw := os.Getenv("REMINDERS_ENABLED"); raw != "" {
		v, err := strconv.ParseBool(raw)
		if err != nil {
			return c, fmt.Errorf("REMINDERS_ENABLED must be true or false")
		}
		c.Enabled = v
	}
	if !c.Enabled {
		return c, nil
	}
	if !regexp.MustCompile(`^[a-z]{2}(?:-[a-z]+)+-\d+$`).MatchString(c.Region) {
		return c, fmt.Errorf("AWS_REGION must name your approved SES region")
	}
	if c.EndpointURL != "" {
		u, err := url.Parse(c.EndpointURL)
		if err != nil || !origin(u) || !loopback(u) || (u.Scheme != "http" && u.Scheme != "https") {
			return c, fmt.Errorf("SES_ENDPOINT_URL must be a loopback HTTP(S) origin for local SES emulation")
		}
		if c.ConfigurationSet != "" {
			return c, fmt.Errorf("unset SES_CONFIGURATION_SET for local SES emulation")
		}
		c.EndpointURL = strings.TrimSuffix(c.EndpointURL, "/")
	}
	a, err := mail.ParseAddress(c.From)
	if err != nil || strings.ContainsAny(c.From, "\r\n") || a.Address == "" {
		return c, fmt.Errorf("REMINDERS_EMAIL_FROM must be a verified SES sender")
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil || !origin(u) || (u.Scheme != "https" && !(c.EndpointURL != "" && u.Scheme == "http" && loopback(u))) {
		return c, fmt.Errorf("email reminders require an HTTPS AUTH_BASE_URL origin (HTTP loopback is allowed only with SES_ENDPOINT_URL)")
	}
	c.BaseURL = strings.TrimSuffix(c.BaseURL, "/")
	if raw := os.Getenv("REMINDER_SEND_INTERVAL"); raw != "" {
		c.Interval, err = time.ParseDuration(raw)
		if err != nil || c.Interval < time.Second || c.Interval > time.Minute {
			return c, fmt.Errorf("REMINDER_SEND_INTERVAL must be between 1s and 1m")
		}
	}
	return c, nil
}

func origin(u *url.URL) bool {
	return u != nil && u.Hostname() != "" && u.User == nil && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && (u.Path == "" || u.Path == "/")
}
func loopback(u *url.URL) bool {
	return u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"
}

type SESClient interface {
	SendEmail(context.Context, *sesv2.SendEmailInput, ...func(*sesv2.Options)) (*sesv2.SendEmailOutput, error)
}
type SES struct {
	client SESClient
	config Config
}

func NewSES(ctx context.Context, c Config) (*SES, error) {
	var cfg aws.Config
	if c.EndpointURL != "" {
		// Local mode does not read real AWS profiles, files or credential chains.
		cfg = aws.Config{Region: c.Region, Credentials: credentials.NewStaticCredentialsProvider("test", "test", ""), RetryMaxAttempts: 1}
	} else {
		var err error
		cfg, err = awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(c.Region), awsconfig.WithRetryMaxAttempts(1))
		if err != nil {
			return nil, fmt.Errorf("SES configuration unavailable")
		}
	}
	implicitEndpoint := false
	client := sesv2.NewFromConfig(cfg, func(o *sesv2.Options) {
		// Do not let implicit AWS SDK environment/profile endpoint overrides
		// bypass the explicit, validated local-emulator mode.
		implicitEndpoint = c.EndpointURL == "" && o.BaseEndpoint != nil
		o.BaseEndpoint = nil
		if c.EndpointURL != "" {
			o.BaseEndpoint = aws.String(c.EndpointURL)
		}
	})
	if implicitEndpoint {
		return nil, fmt.Errorf("implicit AWS SDK SES endpoints are unsupported; unset them or use SES_ENDPOINT_URL for local emulation")
	}
	credentialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if _, err := cfg.Credentials.Retrieve(credentialCtx); err != nil {
		return nil, fmt.Errorf("SES credentials unavailable; configure the AWS SDK credential chain")
	}
	return &SES{client, c}, nil
}
func (s *SES) Send(ctx context.Context, d Delivery) (string, string) {
	zone, err := time.LoadLocation(d.Timezone)
	if err != nil {
		zone = time.UTC
	}
	unsubscribe := s.config.BaseURL + "/reminders/unsubscribe/" + d.UnsubscribeToken
	body := fmt.Sprintf("Public tickets for %s go on sale %s.\n\nTicket page: %s\n\nThis is a public on-sale reminder you requested, not a ticket reservation or a guarantee of availability.\n\nStop this reminder: %s\nManage preferences: %s/me/preferences\n", d.Name, d.SaleAt.In(zone).Format("Monday 2 January 2006, 15:04 MST"), d.URL, unsubscribe, s.config.BaseURL)
	input := &sesv2.SendEmailInput{FromEmailAddress: aws.String(s.config.From), Destination: &types.Destination{ToAddresses: []string{d.To}}, Content: &types.EmailContent{Simple: &types.Message{Subject: &types.Content{Data: aws.String("Public on-sale reminder: " + strings.Join(strings.Fields(d.Name), " ")), Charset: aws.String("UTF-8")}, Body: &types.Body{Text: &types.Content{Data: aws.String(body), Charset: aws.String("UTF-8")}}}}}
	if s.config.ConfigurationSet != "" {
		input.ConfigurationSetName = aws.String(s.config.ConfigurationSet)
	}
	output, err := s.client.SendEmail(ctx, input)
	if err == nil && output != nil && aws.ToString(output.MessageId) != "" {
		return "accepted", aws.ToString(output.MessageId)
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "TooManyRequestsException", "LimitExceededException":
			return "retry", ""
		case "MessageRejected", "BadRequestException", "MailFromDomainNotVerifiedException", "NotFoundException", "AccountSuspendedException", "SendingPausedException", "AccessDeniedException", "UnrecognizedClientException", "InvalidClientTokenId", "SignatureDoesNotMatch":
			return "failed", ""
		}
	}
	// SES has no SendEmail idempotency token. Network errors and 5xx outcomes
	// could mean SES accepted the message; do not automatically send it again.
	return "uncertain", ""
}
