package config

import (
	"fmt"
	"os"
)

type Config struct {
	HTTPAddr           string
	DatabasePath       string
	OpenAIAPIKey       string
	OpenAIModel        string
	EmbeddingModel     string
	ChunkSize          int
	ChunkOverlap       int
	UploadDir          string
	MaxRetrievedChunks int
}

func Load() Config {
	return Config{
		HTTPAddr:           env("HTTP_ADDR", ":8080"),
		DatabasePath:       env("DATABASE_PATH", "data/estimator.db"),
		OpenAIAPIKey:       os.Getenv("OPENAI_API_KEY"),
		OpenAIModel:        env("OPENAI_MODEL", "gpt-4.1-mini"),
		EmbeddingModel:     env("EMBEDDING_MODEL", "text-embedding-3-small"),
		ChunkSize:          envInt("CHUNK_SIZE", 1600),
		ChunkOverlap:       envInt("CHUNK_OVERLAP", 200),
		UploadDir:          env("UPLOAD_DIR", "data/uploads"),
		MaxRetrievedChunks: envInt("MAX_RETRIEVED_CHUNKS", 6),
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if value := os.Getenv(key); value != "" {
		var parsed int
		_, _ = fmt.Sscanf(value, "%d", &parsed)
		if parsed > 0 {
			return parsed
		}
	}
	return fallback
}
