// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package otel provides OpenTelemetry initialisation helpers for the LFX MCP server.
package otel

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/propagators/jaeger"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
)

const (
	// OTelProtocolGRPC is the spec value for the gRPC OTLP transport protocol.
	// Ref: https://opentelemetry.io/docs/specs/otel/protocol/exporter/#specify-protocol
	OTelProtocolGRPC = "grpc"
	// OTelProtocolHTTPProtobuf is the spec value for the HTTP/protobuf OTLP transport protocol.
	OTelProtocolHTTPProtobuf = "http/protobuf"
	// OTelProtocolHTTPJSON is the spec value for the HTTP/JSON OTLP transport protocol.
	// The Go SDK does not support this; it is warned about and treated as http/protobuf.
	OTelProtocolHTTPJSON = "http/json"

	// OTelExporterOTLP configures signals to export via OTLP.
	OTelExporterOTLP = "otlp"
	// OTelExporterNone disables exporting for a signal.
	OTelExporterNone = "none"

	defaultServiceName = "lfx-mcp-server"
)

// Config holds OpenTelemetry configuration options.
type Config struct {
	// ServiceName is the name of the service for resource identification.
	// Env: OTEL_SERVICE_NAME (default: "lfx-mcp-server")
	ServiceName string
	// ServiceVersion is the version of the service.
	// Env: OTEL_SERVICE_VERSION
	ServiceVersion string
	// Protocol specifies the OTLP transport protocol: "grpc", "http/protobuf", or "http/json".
	// Env: OTEL_EXPORTER_OTLP_PROTOCOL (default: "grpc")
	Protocol string
	// Endpoint is the OTLP collector endpoint.
	// For gRPC: typically "http://localhost:4317" or bare "localhost:4317" (http:// is assumed).
	// For HTTP: typically "http://localhost:4318" or bare "localhost:4318" (http:// is assumed).
	// To use TLS, provide an explicit https:// scheme.
	// Env: OTEL_EXPORTER_OTLP_ENDPOINT
	Endpoint string
	// TracesExporter specifies the traces exporter: "otlp" or "none".
	// Env: OTEL_TRACES_EXPORTER (default: "none")
	TracesExporter string
	// TracesSampleRatio specifies the sampling ratio for traces (0.0 to 1.0).
	// Env: OTEL_TRACES_SAMPLE_RATIO (default: 1.0)
	TracesSampleRatio float64
	// MetricsExporter specifies the metrics exporter: "otlp" or "none".
	// Env: OTEL_METRICS_EXPORTER (default: "none")
	MetricsExporter string
	// LogsExporter specifies the logs exporter: "otlp" or "none".
	// Env: OTEL_LOGS_EXPORTER (default: "none")
	LogsExporter string
	// Propagators specifies the propagators to use, comma-separated.
	// Supported values: "tracecontext", "baggage", "jaeger".
	// Env: OTEL_PROPAGATORS (default: "tracecontext,baggage")
	Propagators string
}

// ConfigFromEnv creates a Config from standard OTEL environment variables.
func ConfigFromEnv(serviceVersion string) Config {
	serviceName := os.Getenv("OTEL_SERVICE_NAME")
	if serviceName == "" {
		serviceName = defaultServiceName
	}

	serviceVersionEnv := os.Getenv("OTEL_SERVICE_VERSION")
	if serviceVersionEnv != "" {
		serviceVersion = serviceVersionEnv
	}

	protocol := os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL")
	switch protocol {
	case "":
		protocol = OTelProtocolGRPC
	case OTelProtocolGRPC, OTelProtocolHTTPProtobuf:
		// Valid spec value — keep as-is.
	case OTelProtocolHTTPJSON:
		// The Go SDK does not implement http/json; fall back to http/protobuf.
		slog.Warn("OTEL_EXPORTER_OTLP_PROTOCOL http/json is not supported by the Go SDK, using http/protobuf",
			"provided_value", protocol)
		protocol = OTelProtocolHTTPProtobuf
	default:
		slog.Warn("invalid OTEL_EXPORTER_OTLP_PROTOCOL value, using default",
			"provided_value", protocol, "default", OTelProtocolGRPC)
		protocol = OTelProtocolGRPC
	}

	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")

	tracesExporter := normaliseExporter("OTEL_TRACES_EXPORTER", os.Getenv("OTEL_TRACES_EXPORTER"))
	metricsExporter := normaliseExporter("OTEL_METRICS_EXPORTER", os.Getenv("OTEL_METRICS_EXPORTER"))
	logsExporter := normaliseExporter("OTEL_LOGS_EXPORTER", os.Getenv("OTEL_LOGS_EXPORTER"))

	propagators := os.Getenv("OTEL_PROPAGATORS")
	if propagators == "" {
		propagators = "tracecontext,baggage"
	}

	tracesSampleRatio := 1.0
	if ratio := os.Getenv("OTEL_TRACES_SAMPLE_RATIO"); ratio != "" {
		if parsed, err := strconv.ParseFloat(ratio, 64); err == nil {
			if parsed >= 0.0 && parsed <= 1.0 {
				tracesSampleRatio = parsed
			} else {
				slog.Warn("OTEL_TRACES_SAMPLE_RATIO must be between 0.0 and 1.0, using default 1.0",
					"provided_value", ratio)
			}
		} else {
			slog.Warn("invalid OTEL_TRACES_SAMPLE_RATIO value, using default 1.0",
				"provided_value", ratio, "error", err)
		}
	}

	slog.With(
		"service_name", serviceName,
		"service_version", serviceVersion,
		"protocol", protocol,
		"endpoint", endpoint,
		"traces_exporter", tracesExporter,
		"traces_sample_ratio", tracesSampleRatio,
		"metrics_exporter", metricsExporter,
		"logs_exporter", logsExporter,
		"propagators", propagators,
	).Debug("OTel config resolved")

	return Config{
		ServiceName:       serviceName,
		ServiceVersion:    serviceVersion,
		Protocol:          protocol,
		Endpoint:          endpoint,
		TracesExporter:    tracesExporter,
		TracesSampleRatio: tracesSampleRatio,
		MetricsExporter:   metricsExporter,
		LogsExporter:      logsExporter,
		Propagators:       propagators,
	}
}

// SetupSDK bootstraps the OpenTelemetry pipeline using environment variables.
// Call the returned shutdown function (e.g. via defer) for proper cleanup.
func SetupSDK(ctx context.Context, serviceVersion string) (shutdown func(context.Context) error, err error) {
	return SetupSDKWithConfig(ctx, ConfigFromEnv(serviceVersion))
}

// SetupSDKWithConfig bootstraps the OpenTelemetry pipeline with the provided configuration.
// Call the returned shutdown function (e.g. via defer) for proper cleanup.
func SetupSDKWithConfig(ctx context.Context, cfg Config) (shutdown func(context.Context) error, err error) {
	var shutdownFuncs []func(context.Context) error

	// shutdown calls cleanup functions registered via shutdownFuncs.
	// All errors are joined; each registered cleanup is invoked exactly once.
	shutdown = func(ctx context.Context) error {
		var err error
		for _, fn := range shutdownFuncs {
			err = errors.Join(err, fn(ctx))
		}
		shutdownFuncs = nil
		return err
	}

	handleErr := func(inErr error) {
		err = errors.Join(inErr, shutdown(ctx))
	}

	// Normalise endpoint to include a URL scheme so WithEndpointURL can parse it.
	// Bare host:port values like "127.0.0.1:4317" cause url.Parse to fail with
	// "first path segment in URL cannot contain colon". http:// is assumed for
	// bare values; users requiring TLS must supply an explicit https:// scheme.
	if cfg.Endpoint != "" {
		cfg.Endpoint = endpointURL(cfg.Endpoint)
	}

	// Route OTEL internal errors through slog instead of the default stderr writer.
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		slog.Error("OTel internal error", "error", err)
	}))

	res, err := newResource(cfg)
	if err != nil {
		handleErr(err)
		return
	}

	otel.SetTextMapPropagator(newPropagator(cfg))

	// cfg.TracesExporter is already validated to "otlp" or "none" by ConfigFromEnv.
	if cfg.TracesExporter != OTelExporterNone {
		var tracerProvider *trace.TracerProvider
		tracerProvider, err = newTraceProvider(ctx, cfg, res)
		if err != nil {
			handleErr(err)
			return
		}
		shutdownFuncs = append(shutdownFuncs, tracerProvider.Shutdown)
		otel.SetTracerProvider(tracerProvider)
	}

	if cfg.MetricsExporter != OTelExporterNone {
		var metricsProvider *metric.MeterProvider
		metricsProvider, err = newMetricsProvider(ctx, cfg, res)
		if err != nil {
			handleErr(err)
			return
		}
		shutdownFuncs = append(shutdownFuncs, metricsProvider.Shutdown)
		otel.SetMeterProvider(metricsProvider)
	}

	if cfg.LogsExporter != OTelExporterNone {
		var loggerProvider *log.LoggerProvider
		loggerProvider, err = newLoggerProvider(ctx, cfg, res)
		if err != nil {
			handleErr(err)
			return
		}
		shutdownFuncs = append(shutdownFuncs, loggerProvider.Shutdown)
		global.SetLoggerProvider(loggerProvider)
	}

	return
}

// endpointURL ensures the endpoint has a URL scheme. The OTel SDK passes the
// endpoint to url.Parse internally, which rejects bare host:port values like
// "127.0.0.1:4317" with "first path segment in URL cannot contain colon".
// Bare values are assumed to be plain HTTP; supply an explicit https:// scheme
// to use TLS.
func endpointURL(raw string) string {
	if strings.Contains(raw, "://") {
		return raw
	}
	return "http://" + raw
}

// newResource creates an OpenTelemetry resource with service name and version attributes.
func newResource(cfg Config) (*resource.Resource, error) {
	return resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
		),
	)
}

// newPropagator creates a composite text map propagator from the configured propagator list.
// Supported values: "tracecontext", "baggage", "jaeger".
func newPropagator(cfg Config) propagation.TextMapPropagator {
	var propagators []propagation.TextMapPropagator
	for _, p := range strings.Split(cfg.Propagators, ",") {
		switch strings.TrimSpace(p) {
		case "tracecontext":
			propagators = append(propagators, propagation.TraceContext{})
		case "baggage":
			propagators = append(propagators, propagation.Baggage{})
		case "jaeger":
			propagators = append(propagators, jaeger.Jaeger{})
		default:
			slog.Warn("unknown OTel propagator, skipping", "propagator", p)
		}
	}
	if len(propagators) == 0 {
		// Default to W3C TraceContext + Baggage when no valid propagators are configured.
		propagators = append(propagators, propagation.TraceContext{}, propagation.Baggage{})
	}
	return propagation.NewCompositeTextMapPropagator(propagators...)
}

// normaliseExporter validates a signal exporter env var value, accepting only
// "otlp" and "none". Any other value emits a warning and defaults to "none".
func normaliseExporter(envKey, v string) string {
	switch v {
	case "":
		return OTelExporterNone
	case OTelExporterOTLP, OTelExporterNone:
		return v
	default:
		slog.Warn("unsupported exporter value, only \"otlp\" and \"none\" are supported; using \"none\"",
			"env", envKey, "provided_value", v)
		return OTelExporterNone
	}
}

// newTraceProvider creates a TracerProvider with the configured OTLP exporter.
func newTraceProvider(ctx context.Context, cfg Config, res *resource.Resource) (*trace.TracerProvider, error) {
	var exporter trace.SpanExporter
	var err error

	if cfg.Protocol == OTelProtocolHTTPProtobuf {
		opts := []otlptracehttp.Option{}
		if cfg.Endpoint != "" {
			opts = append(opts, otlptracehttp.WithEndpointURL(cfg.Endpoint))
		}
		exporter, err = otlptracehttp.New(ctx, opts...)
	} else {
		opts := []otlptracegrpc.Option{}
		if cfg.Endpoint != "" {
			opts = append(opts, otlptracegrpc.WithEndpointURL(cfg.Endpoint))
		}
		exporter, err = otlptracegrpc.New(ctx, opts...)
	}

	if err != nil {
		return nil, err
	}

	return trace.NewTracerProvider(
		trace.WithResource(res),
		trace.WithSampler(trace.ParentBased(trace.TraceIDRatioBased(cfg.TracesSampleRatio))),
		trace.WithBatcher(exporter,
			trace.WithBatchTimeout(time.Second),
		),
	), nil
}

// newMetricsProvider creates a MeterProvider with the configured OTLP exporter.
func newMetricsProvider(ctx context.Context, cfg Config, res *resource.Resource) (*metric.MeterProvider, error) {
	var exporter metric.Exporter
	var err error

	if cfg.Protocol == OTelProtocolHTTPProtobuf {
		opts := []otlpmetrichttp.Option{}
		if cfg.Endpoint != "" {
			opts = append(opts, otlpmetrichttp.WithEndpointURL(cfg.Endpoint))
		}
		exporter, err = otlpmetrichttp.New(ctx, opts...)
	} else {
		opts := []otlpmetricgrpc.Option{}
		if cfg.Endpoint != "" {
			opts = append(opts, otlpmetricgrpc.WithEndpointURL(cfg.Endpoint))
		}
		exporter, err = otlpmetricgrpc.New(ctx, opts...)
	}

	if err != nil {
		return nil, err
	}

	return metric.NewMeterProvider(
		metric.WithResource(res),
		metric.WithReader(metric.NewPeriodicReader(exporter,
			metric.WithInterval(30*time.Second),
		)),
	), nil
}

// newLoggerProvider creates a LoggerProvider with the configured OTLP exporter.
func newLoggerProvider(ctx context.Context, cfg Config, res *resource.Resource) (*log.LoggerProvider, error) {
	var exporter log.Exporter
	var err error

	if cfg.Protocol == OTelProtocolHTTPProtobuf {
		opts := []otlploghttp.Option{}
		if cfg.Endpoint != "" {
			opts = append(opts, otlploghttp.WithEndpointURL(cfg.Endpoint))
		}
		exporter, err = otlploghttp.New(ctx, opts...)
	} else {
		opts := []otlploggrpc.Option{}
		if cfg.Endpoint != "" {
			opts = append(opts, otlploggrpc.WithEndpointURL(cfg.Endpoint))
		}
		exporter, err = otlploggrpc.New(ctx, opts...)
	}

	if err != nil {
		return nil, err
	}

	return log.NewLoggerProvider(
		log.WithResource(res),
		log.WithProcessor(log.NewBatchProcessor(exporter)),
	), nil
}
