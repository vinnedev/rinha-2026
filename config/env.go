package config

import (
	"strconv"
	"time"

	"github.com/vinnedev/rinha-2026/pkg/dotenv"
)

var (
	ENV_MODE    = dotenv.GetEnv("ENV_MODE", "development")
	SERVICE     = dotenv.GetEnv("SERVICE_NAME", "rinha-2026")
	HOST        = dotenv.GetEnv("HOST", "0.0.0.0")
	PORT        = dotenv.GetEnv("PORT", "8080")
	ADMIN_PORT  = dotenv.GetEnv("ADMIN_PORT", "9090")
	ENABLE_PPROF = parseBool("ENABLE_PPROF", false)

	LOG_LEVEL  = dotenv.GetEnv("LOG_LEVEL", "info")
	LOG_FORMAT = dotenv.GetEnv("LOG_FORMAT", "json")

	READ_TIMEOUT        = parseDuration("READ_TIMEOUT", 5*time.Second)
	READ_HEADER_TIMEOUT = parseDuration("READ_HEADER_TIMEOUT", 2*time.Second)
	WRITE_TIMEOUT       = parseDuration("WRITE_TIMEOUT", 10*time.Second)
	IDLE_TIMEOUT        = parseDuration("IDLE_TIMEOUT", 120*time.Second)
	SHUTDOWN_TIMEOUT    = parseDuration("SHUTDOWN_TIMEOUT", 30*time.Second)
	TCP_KEEPALIVE       = parseDuration("TCP_KEEPALIVE", 3*time.Minute)

	MAX_HEADER_BYTES = parseInt("MAX_HEADER_BYTES", 1<<20)
	TRUSTED_PROXIES  = dotenv.GetEnv("TRUSTED_PROXIES", "")

	DATASET_PATH = dotenv.GetEnv("DATASET_PATH", "/data/vectors.bin")
	TREE_PATH    = dotenv.GetEnv("TREE_PATH", "/data/fraud_dt.bin")

	HYBRID_ENABLED   = parseBool("HYBRID_ENABLED", true)
	HYBRID_LO        = parseFloat("HYBRID_LO", 0.2)
	HYBRID_HI        = parseFloat("HYBRID_HI", 0.8)

	SOCKET_PATH = dotenv.GetEnv("SOCKET_PATH", "")

	WARMUP_ITERS = parseInt("WARMUP_ITERS", 500)

	STEADY_GC_OFF      = parseBool("STEADY_GC_OFF", false)
	STEADY_GC_INTERVAL = parseDuration("STEADY_GC_INTERVAL", 5*time.Second)

	SHED_SLOTS   = parseInt("SHED_SLOTS", 0)
	SHED_TIMEOUT = parseDuration("SHED_TIMEOUT", 3*time.Millisecond)

	USE_RAWHTTP = parseBool("USE_RAWHTTP", false)
)

func IsProduction() bool {
	return ENV_MODE == "production"
}

func parseDuration(key string, fallback time.Duration) time.Duration {
	v := dotenv.GetEnv(key, "")
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}

func parseInt(key string, fallback int) int {
	v := dotenv.GetEnv(key, "")
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func parseBool(key string, fallback bool) bool {
	v := dotenv.GetEnv(key, "")
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func parseFloat(key string, fallback float64) float64 {
	v := dotenv.GetEnv(key, "")
	if v == "" {
		return fallback
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fallback
	}
	return f
}
