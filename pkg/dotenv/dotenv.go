package dotenv

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/joho/godotenv"
)

func ReadEnvFromFile(key, fallback string, file *string) string {
	envPath := ".env"
	if file != nil {
		envPath = *file
	}

	err := godotenv.Load(envPath)
	if err != nil {
		return fallback
	}

	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func GetEnv(key, fallback string) string {
	_, currentPath, _, _ := runtime.Caller(0)
	basepath := filepath.Join(filepath.Dir(currentPath), "../../.env")

	if value := os.Getenv(key); value != "" {
		return value
	}

	return ReadEnvFromFile(key, fallback, &basepath)
}
