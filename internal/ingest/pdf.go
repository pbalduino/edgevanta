package ingest

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ledongthuc/pdf"
)

func ExtractPDFPages(path string) ([]PageText, int, error) {
	file, reader, err := pdf.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()

	totalPages := reader.NumPage()
	pages := make([]PageText, 0, totalPages)
	log.Printf("pdf ingest: scanning %s (%d pages)", filepath.Base(path), totalPages)

	for i := 1; i <= totalPages; i++ {
		log.Printf("pdf ingest: %s page %d/%d", filepath.Base(path), i, totalPages)
		page := reader.Page(i)
		if page.V.IsNull() {
			log.Printf("pdf ingest: %s page %d/%d skipped (null page)", filepath.Base(path), i, totalPages)
			continue
		}

		fonts := map[string]*pdf.Font{}
		for _, name := range page.Fonts() {
			font := page.Font(name)
			fonts[name] = &font
		}

		text, err := page.GetPlainText(fonts)
		if err == nil {
			pageText := normalizePDFText(text)
			if pageText != "" {
				log.Printf("pdf ingest: %s page %d/%d extracted with native text", filepath.Base(path), i, totalPages)
				pages = append(pages, PageText{
					Page: i,
					Text: fmt.Sprintf("[Page %d]\n%s", i, pageText),
				})
				continue
			}
		}

		log.Printf("pdf ingest: %s page %d/%d falling back to OCR", filepath.Base(path), i, totalPages)
		ocrText, ocrErr := ocrPDFPage(path, i)
		if ocrErr != nil {
			log.Printf("pdf ingest: %s page %d/%d OCR failed: %v", filepath.Base(path), i, totalPages, ocrErr)
			continue
		}
		ocrText = normalizePDFText(ocrText)
		if ocrText == "" {
			log.Printf("pdf ingest: %s page %d/%d OCR returned no text", filepath.Base(path), i, totalPages)
			continue
		}
		log.Printf("pdf ingest: %s page %d/%d extracted with OCR", filepath.Base(path), i, totalPages)
		pages = append(pages, PageText{
			Page: i,
			Text: fmt.Sprintf("[Page %d]\n%s", i, ocrText),
		})
	}

	log.Printf("pdf ingest: completed %s with %d extracted pages out of %d", filepath.Base(path), len(pages), totalPages)
	return pages, totalPages, nil
}

func ExtractPDFText(path string) (string, error) {
	file, reader, err := pdf.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	rawReader, err := reader.GetPlainText()
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(rawReader); err != nil {
		return "", err
	}
	return normalizePDFText(buf.String()), nil
}

func normalizePDFText(text string) string {
	lines := strings.Split(text, "\n")
	normalized := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		normalized = append(normalized, line)
	}
	return strings.Join(normalized, "\n")
}

func ocrPDFPage(path string, page int) (string, error) {
	if _, err := exec.LookPath("gs"); err != nil {
		return "", fmt.Errorf("ghostscript not available for OCR fallback: %w", err)
	}
	if _, err := exec.LookPath("tesseract"); err != nil {
		return "", fmt.Errorf("tesseract not available for OCR fallback: %w", err)
	}

	tempDir, err := os.MkdirTemp("", "plans-ocr-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tempDir)

	imagePath := filepath.Join(tempDir, fmt.Sprintf("page-%03d.png", page))
	gsCmd := exec.Command(
		"gs",
		"-q",
		"-dNOPAUSE",
		"-dBATCH",
		"-sDEVICE=png16m",
		"-r200",
		fmt.Sprintf("-dFirstPage=%d", page),
		fmt.Sprintf("-dLastPage=%d", page),
		fmt.Sprintf("-sOutputFile=%s", imagePath),
		path,
	)
	if output, err := gsCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("ghostscript render failed: %w: %s", err, strings.TrimSpace(string(output)))
	}

	ocrCmd := exec.Command("tesseract", imagePath, "stdout")
	output, err := ocrCmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("tesseract OCR failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}
