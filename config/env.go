package config

import (
	"strconv"

	"github.com/vinnedev/rinha-2026/pkg/dotenv"
)

var (
	ENV_MODE = dotenv.GetEnv("ENV_MODE", "development")
	SERVICE  = dotenv.GetEnv("SERVICE_NAME", "rinha-2026")
	HOST     = dotenv.GetEnv("HOST", "0.0.0.0")
	PORT     = dotenv.GetEnv("PORT", "8080")

	LOG_LEVEL  = dotenv.GetEnv("LOG_LEVEL", "info")
	LOG_FORMAT = dotenv.GetEnv("LOG_FORMAT", "json")

	DATASET_PATH = dotenv.GetEnv("DATASET_PATH", "/data/vectors.bin")
	TREE_PATH    = dotenv.GetEnv("TREE_PATH", "/data/fraud_dt.bin")

	HYBRID_ENABLED = parseBool("HYBRID_ENABLED", true)
	HYBRID_LO      = parseFloat("HYBRID_LO", 0.2)
	HYBRID_HI      = parseFloat("HYBRID_HI", 0.8)

	SOCKET_PATH      = dotenv.GetEnv("SOCKET_PATH", "")
	CTRL_SOCKET_PATH = dotenv.GetEnv("CTRL_SOCKET_PATH", "")

	WARMUP_ITERS = parseInt("WARMUP_ITERS", 500)

	STEADY_GC_OFF = parseBool("STEADY_GC_OFF", true)
)

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
