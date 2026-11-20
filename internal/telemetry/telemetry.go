// Package telemetry exports Ticketopia's metrics and traces over OTLP/HTTP
// when an OpenTelemetry endpoint is configured. Logs stay structured JSON
// on stdout for a collector to read.
//
// Instruments use the global meter provider, so packages may create them
// before Setup runs. Like the metrics logs, they carry fixed dimensions only:
// never accounts, IPs, tokens, arguments or event data.
package telemetry

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	rtmetrics "runtime/metrics"
	"strconv"
	"strings"
	"sync"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/exemplar"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Scope names the instrumentation scope for Ticketopia's own instruments.
const Scope = "github.com/sonastea/ticketopia"

// Meter is the meter for Ticketopia's instruments.
func Meter() metric.Meter { return otel.Meter(Scope) }

// DurationBuckets (seconds) match the metrics logs' duration buckets.
var DurationBuckets = []float64{0.001, 0.01, 0.05, 0.25, 1, 5}

// Config is the OpenTelemetry environment this package acts on. The exporter
// itself reads the rest of the standard variables (headers, timeout,
// compression, TLS) and the reader reads OTEL_METRIC_EXPORT_INTERVAL.
type Config struct {
	// Endpoints are empty for disabled signals. Exporters read the corresponding
	// environment variables directly to resolve paths, headers and TLS.
	MetricsEndpoint string
	TracesEndpoint  string
}

// ConfigFromEnv enables each signal when an OTLP endpoint is set, unless
// OTEL_SDK_DISABLED=true or the signal's exporter is set to none. Only OTLP over
// HTTP/protobuf is supported, the protocol SigNoz and collectors accept on
// port 4318; other exporters or protocols are a configuration error rather
// than silently exporting nothing.
func ConfigFromEnv() (Config, error) {
	var c Config
	if raw := os.Getenv("OTEL_SDK_DISABLED"); raw != "" {
		disabled, err := strconv.ParseBool(raw)
		if err != nil {
			return c, errors.New("OTEL_SDK_DISABLED must be true or false")
		}
		if disabled {
			return c, nil
		}
	}
	var err error
	if c.MetricsEndpoint, err = signalEndpoint("METRICS"); err != nil {
		return Config{}, err
	}
	if c.TracesEndpoint, err = signalEndpoint("TRACES"); err != nil {
		return Config{}, err
	}
	return c, nil
}

func signalEndpoint(signal string) (string, error) {
	exporterVar := "OTEL_" + signal + "_EXPORTER"
	switch exporter := strings.TrimSpace(os.Getenv(exporterVar)); exporter {
	case "", "otlp":
	case "none":
		return "", nil
	default:
		return "", fmt.Errorf("%s=%s is not supported; use otlp or none", exporterVar, exporter)
	}
	endpoint := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_" + signal + "_ENDPOINT"))
	if endpoint == "" {
		endpoint = strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	}
	if endpoint == "" {
		return "", nil
	}
	for _, name := range []string{"OTEL_EXPORTER_OTLP_" + signal + "_PROTOCOL", "OTEL_EXPORTER_OTLP_PROTOCOL"} {
		if p := strings.TrimSpace(os.Getenv(name)); p != "" {
			if p != "http/protobuf" {
				return "", fmt.Errorf("%s=%s is not supported; use http/protobuf (port 4318)", name, p)
			}
			break
		}
	}
	return endpoint, nil
}

// Setup installs the configured global providers and returns a function that
// flushes and stops them. An unreachable collector never stops startup.
func Setup(ctx context.Context, c Config, version string, logger zerolog.Logger) (func(context.Context) error, error) {
	if c.MetricsEndpoint == "" && c.TracesEndpoint == "" {
		return func(context.Context) error { return nil }, nil
	}
	host, _ := os.Hostname()
	// Later options win: OTEL_SERVICE_NAME and OTEL_RESOURCE_ATTRIBUTES
	// override these defaults.
	res, err := resource.New(ctx,
		resource.WithAttributes(attribute.String("service.name", "ticketopia"), attribute.String("service.version", version),
			attribute.String("service.instance.id", host)),
		resource.WithTelemetrySDK(), resource.WithFromEnv())
	if err != nil {
		return nil, fmt.Errorf("telemetry resource: %w", err)
	}
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		logger.Warn().Err(err).Msg("Telemetry export failed")
	}))

	var meterProvider *sdkmetric.MeterProvider
	if c.MetricsEndpoint != "" {
		exporter, err := otlpmetrichttp.New(ctx)
		if err != nil {
			return nil, fmt.Errorf("OTLP metrics exporter: %w", err)
		}
		// The SDK considers spans sampled before we decide to export them.
		// Don't attach metric exemplars that could link to discarded traces.
		meterProvider = sdkmetric.NewMeterProvider(sdkmetric.WithResource(res),
			sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter)), sdkmetric.WithExemplarFilter(exemplar.AlwaysOffFilter))
		if err := observeProcess(meterProvider.Meter(Scope)); err != nil {
			return nil, errors.Join(err, meterProvider.Shutdown(ctx))
		}
	}
	var stop []func(context.Context) error
	if c.TracesEndpoint != "" {
		exporter, err := otlptracehttp.New(ctx)
		if err != nil {
			if meterProvider != nil {
				err = errors.Join(err, meterProvider.Shutdown(ctx))
			}
			return nil, fmt.Errorf("OTLP traces exporter: %w", err)
		}
		// Mark failures in instrumentation; the SDK must not export panic values.
		provider := sdktrace.NewTracerProvider(sdktrace.WithResource(res), sdktrace.WithoutPanicRecording(),
			sdktrace.WithSampler(sdktrace.AlwaysSample()),
			sdktrace.WithSpanProcessor(newTraceFilter(sdktrace.NewBatchSpanProcessor(exporter))))
		otel.SetTracerProvider(provider)
		stop = append(stop, provider.Shutdown)
	}
	if meterProvider != nil {
		otel.SetMeterProvider(meterProvider)
		stop = append(stop, meterProvider.Shutdown)
	}
	return func(ctx context.Context) error {
		// Both signals get the full shutdown budget even if one endpoint stalls.
		errs := make([]error, len(stop))
		var wg sync.WaitGroup
		for i, shutdown := range stop {
			wg.Go(func() { errs[i] = shutdown(ctx) })
		}
		wg.Wait()
		return errors.Join(errs...)
	}, nil
}

// observeProcess reports goroutines and live heap, as the live activity
// metrics log does, for every deployment.
func observeProcess(meter metric.Meter) error {
	goroutines, err := meter.Int64ObservableGauge("ticketopia.process.goroutines", metric.WithUnit("{goroutine}"),
		metric.WithDescription("Live goroutines."))
	if err != nil {
		return err
	}
	heap, err := meter.Int64ObservableGauge("ticketopia.process.heap_objects", metric.WithUnit("By"),
		metric.WithDescription("Bytes of live and not yet swept heap objects."))
	if err != nil {
		return err
	}
	_, err = meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		samples := []rtmetrics.Sample{{Name: "/sched/goroutines:goroutines"}, {Name: "/memory/classes/heap/objects:bytes"}}
		rtmetrics.Read(samples)
		o.ObserveInt64(goroutines, int64(samples[0].Value.Uint64()))
		o.ObserveInt64(heap, int64(samples[1].Value.Uint64()))
		return nil
	}, goroutines, heap)
	return err
}

// ObserveDB reports the shared SQL pool's connections and waits.
func ObserveDB(meter metric.Meter, stats func() sql.DBStats) error {
	connections, err := meter.Int64ObservableGauge("ticketopia.db.connections", metric.WithUnit("{connection}"),
		metric.WithDescription("Open SQL connections by state (in_use, idle)."))
	if err != nil {
		return err
	}
	limit, err := meter.Int64ObservableGauge("ticketopia.db.connections.max", metric.WithUnit("{connection}"),
		metric.WithDescription("Maximum open SQL connections."))
	if err != nil {
		return err
	}
	waits, err := meter.Int64ObservableCounter("ticketopia.db.connection.waits", metric.WithUnit("{wait}"),
		metric.WithDescription("Requests that waited for a free SQL connection."))
	if err != nil {
		return err
	}
	waited, err := meter.Float64ObservableCounter("ticketopia.db.connection.wait_time", metric.WithUnit("s"),
		metric.WithDescription("Total time spent waiting for a free SQL connection."))
	if err != nil {
		return err
	}
	inUse, idle := metric.WithAttributes(attribute.String("state", "in_use")), metric.WithAttributes(attribute.String("state", "idle"))
	_, err = meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		s := stats()
		o.ObserveInt64(connections, int64(s.InUse), inUse)
		o.ObserveInt64(connections, int64(s.Idle), idle)
		o.ObserveInt64(limit, int64(s.MaxOpenConnections))
		o.ObserveInt64(waits, s.WaitCount)
		o.ObserveFloat64(waited, s.WaitDuration.Seconds())
		return nil
	}, connections, limit, waits, waited)
	return err
}
