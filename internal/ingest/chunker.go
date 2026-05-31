package ingest

import "strings"

type Chunk struct {
	Text      string
	PageStart int
	PageEnd   int
	Section   string
}

type PageText struct {
	Page int
	Text string
}

func ChunkPages(pages []PageText, chunkSize, overlap int) []Chunk {
	if chunkSize <= 0 {
		chunkSize = 1600
	}
	if overlap < 0 {
		overlap = 0
	}

	var chunks []Chunk
	var builder strings.Builder
	pageStart := 0
	lastPage := 0
	currentSize := 0

	flush := func() {
		text := strings.TrimSpace(builder.String())
		if text == "" {
			return
		}
		chunks = append(chunks, Chunk{
			Text:      text,
			PageStart: pageStart,
			PageEnd:   lastPage,
		})
	}

	for _, page := range pages {
		text := strings.TrimSpace(page.Text)
		if text == "" {
			continue
		}
		if builder.Len() == 0 {
			pageStart = page.Page
		}

		if currentSize+len(text) > chunkSize && builder.Len() > 0 {
			flush()
			previous := builder.String()
			builder.Reset()
			if overlap > 0 && len(previous) > overlap {
				builder.WriteString(previous[len(previous)-overlap:])
				builder.WriteString("\n")
			}
			pageStart = page.Page
			currentSize = builder.Len()
		}

		builder.WriteString(text)
		builder.WriteString("\n")
		currentSize = builder.Len()
		lastPage = page.Page
	}

	flush()
	return chunks
}
