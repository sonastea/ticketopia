package api

import (
	"context"
	_ "embed"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/a-h/templ"
	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/events"
	"github.com/sonastea/ticketopia/internal/kv"
	"github.com/sonastea/ticketopia/internal/location"
	"github.com/sonastea/ticketopia/internal/saved"
	"github.com/sonastea/ticketopia/views/assets"
)

//go:embed openapi.yaml
var openAPI []byte

type api struct {
	logger      zerolog.Logger
	events      *discovery.Service
	locations   *location.Resolver
	ipExtractor echo.IPExtractor
	shutdown    <-chan struct{}
	ready       func(context.Context) error
	durable     *events.Service
	accounts    *accounts.Service
	authConfig  accounts.Config
	saved       *saved.Service
}

type Option func(*api)

func WithSavedEvents(repository saved.Repository) Option {
	return func(a *api) { a.saved = saved.New(repository, a.events) }
}

func WithAccounts(config accounts.Config, service *accounts.Service) Option {
	return func(a *api) { a.authConfig, a.accounts = config, service }
}

// WithPersistence wires shared services without changing read-only discovery.
func WithPersistence(ready func(context.Context) error, durable *events.Service) Option {
	return func(a *api) { a.ready, a.durable = ready, durable }
}

func NewAPI(ctx context.Context, logger zerolog.Logger, cache kv.Store, options ...Option) (*api, error) {
	budget := 4500
	if value := os.Getenv("TICKETMASTER_DAILY_BUDGET"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 {
			return nil, fmt.Errorf("TICKETMASTER_DAILY_BUDGET must be a positive integer")
		}
		budget = parsed
	}
	ipExtractor, err := clientIPExtractor(os.Getenv("TRUSTED_PROXY_CIDRS"))
	if err != nil {
		return nil, err
	}
	geolocation := true
	if value := os.Getenv("IP_GEOLOCATION_ENABLED"); value != "" {
		geolocation, err = strconv.ParseBool(value)
		if err != nil {
			return nil, fmt.Errorf("IP_GEOLOCATION_ENABLED must be true or false")
		}
	}
	a := &api{
		logger: logger, ipExtractor: ipExtractor, shutdown: ctx.Done(),
		events: discovery.New(ctx, cache, logger, discovery.Config{
			APIKey: os.Getenv("TICKETMASTER_KEY"), DailyBudget: budget,
		}),
		locations: location.New(ctx, cache, logger, location.Config{Disabled: !geolocation}),
	}
	for _, option := range options {
		option(a)
	}
	if err := a.authConfig.Validate(); err != nil {
		return nil, err
	}
	if a.authConfig.Enabled && a.accounts == nil {
		return nil, fmt.Errorf("authentication requires an account service")
	}
	return a, nil
}

func (a *api) Server(port int) *http.Server {
	return &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           a.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       time.Minute,
	}
}

func (a *api) Routes() *echo.Echo {
	e := echo.New()
	e.IPExtractor = a.ipExtractor
	if e.IPExtractor == nil {
		e.IPExtractor = echo.ExtractIPDirect()
	}

	e.GET("/healthz", healthHandler)
	e.GET("/readyz", a.readinessHandler)
	e.GET("/", a.retrieveEventsHandler)
	e.GET("/events/:event_id", a.eventPageHandler)
	a.savedRoutes(e)
	e.GET("/community", destinationHandler("community", "Community"))
	a.accountRoutes(e)
	e.GET("/assets/*", echo.WrapHandler(http.StripPrefix("/assets/", http.FileServer(http.FS(assets.Files)))))
	e.GET("/api/v1/events", a.eventsHandler)
	e.GET("/api/v1/events/:event_id", a.eventHandler)
	e.GET("/api/v1/genres", a.genresHandler)
	e.GET("/api/v1/categories", a.categoriesHandler)
	e.GET("/api/v1/openapi.yaml", func(c echo.Context) error {
		if len(c.QueryParams()) > 0 {
			return problem(c, &discovery.ValidationError{Field: "query", Message: "the API contract does not accept filters"})
		}
		return c.Blob(http.StatusOK, "application/yaml", openAPI)
	})

	return e
}

func render(ctx echo.Context, status int, t templ.Component) error {
	ctx.Response().Header().Set(echo.HeaderContentType, echo.MIMETextHTMLCharsetUTF8)
	ctx.Response().Writer.WriteHeader(status)
	return t.Render(ctx.Request().Context(), ctx.Response().Writer)
}
