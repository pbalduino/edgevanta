package app

import (
	"context"
	"encoding/json"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/pbalduino/ev_assignment/internal/config"
	"github.com/pbalduino/ev_assignment/internal/domain"
	"github.com/pbalduino/ev_assignment/internal/provider"
	"github.com/pbalduino/ev_assignment/internal/service"
	"github.com/pbalduino/ev_assignment/internal/storage"
)

type App struct {
	cfg      config.Config
	service  *service.EstimatorService
	template *template.Template
}

func New(cfg config.Config) (*App, error) {
	store, err := storage.NewSQLiteStore(cfg.DatabasePath)
	if err != nil {
		return nil, err
	}

	openaiClient, err := provider.NewOpenAIClient(cfg.OpenAIAPIKey, cfg.EmbeddingModel, cfg.OpenAIModel)
	if err != nil {
		return nil, err
	}

	svc := service.NewEstimatorService(cfg, store, openaiClient)
	if err := svc.Bootstrap(context.Background()); err != nil {
		return nil, err
	}

	tpl, err := template.New("index").Parse(indexHTML)
	if err != nil {
		return nil, err
	}

	return &App{
		cfg:      cfg,
		service:  svc,
		template: tpl,
	}, nil
}

func (a *App) Router() http.Handler {
	r := chi.NewRouter()

	r.Get("/", a.handleIndex)
	r.Get("/healthz", a.handleHealth)
	r.Get("/api/documents", a.handleListDocuments)
	r.Post("/api/ask", a.handleAsk)
	r.Post("/api/ingest", a.handleIngest)
	r.Post("/api/upload", a.handleUpload)
	r.Get("/api/tools/analyze-bid-data", a.handleAnalyzeBidData)
	r.Get("/api/tools/price-outliers", a.handlePriceOutliers)
	r.Get("/api/tools/project-summary", a.handleProjectSummary)
	r.Get("/api/tools/search", a.handleSearchDocuments)

	return r
}

func (a *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	docs, err := a.service.ListDocuments(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := a.template.Execute(w, map[string]any{
		"documents": docs,
		"addr":      a.cfg.HTTPAddr,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (a *App) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *App) handleListDocuments(w http.ResponseWriter, r *http.Request) {
	docs, err := a.service.ListDocuments(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, docs)
}

func (a *App) handleAsk(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Question string `json:"question"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	req.Question = strings.TrimSpace(req.Question)
	if req.Question == "" {
		http.Error(w, "question is required", http.StatusBadRequest)
		return
	}

	resp, err := a.service.Ask(r.Context(), req.Question)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *App) handleIngest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Path) == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}
	if !filepath.IsAbs(req.Path) && !strings.HasPrefix(req.Path, "docs/") {
		req.Path = filepath.Clean(req.Path)
	}
	if err := a.service.IngestFile(r.Context(), req.Path); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "ingested", "path": req.Path})
}

func (a *App) handleUpload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file is required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	filename := filepath.Base(header.Filename)
	targetPath := filepath.Join(a.cfg.UploadDir, filename)
	dst, err := os.Create(targetPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := a.service.IngestFile(r.Context(), targetPath); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "uploaded", "path": targetPath})
}

func (a *App) handleAnalyzeBidData(w http.ResponseWriter, r *http.Request) {
	result, err := a.service.AnalyzeBidData(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *App) handlePriceOutliers(w http.ResponseWriter, r *http.Request) {
	result, err := a.service.FindPriceOutliers(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *App) handleProjectSummary(w http.ResponseWriter, r *http.Request) {
	result, err := a.service.GetProjectSummary(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *App) handleSearchDocuments(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		http.Error(w, "q is required", http.StatusBadRequest)
		return
	}
	source := domain.KnowledgeSource(strings.TrimSpace(r.URL.Query().Get("source")))
	chunks, err := a.service.SearchDocuments(r.Context(), query, source)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, chunks)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

const indexHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Construction Estimating Agent</title>
  <style>
    :root { color-scheme: light; --bg:#f4f1e8; --panel:#fffdf8; --ink:#1f2937; --accent:#9c6644; --line:#d9c7b8; }
    body { margin:0; font-family: Georgia, "Times New Roman", serif; background:linear-gradient(180deg,#ede6db 0%,#f8f5ef 100%); color:var(--ink); }
    main { max-width:1000px; margin:0 auto; padding:32px 20px 48px; }
    h1,h2 { margin:0 0 12px; }
    .grid { display:grid; gap:20px; grid-template-columns: 1.2fr 0.8fr; }
    .card { background:var(--panel); border:1px solid var(--line); border-radius:16px; padding:20px; box-shadow:0 8px 24px rgba(31,41,55,.06); }
    .stack { display:grid; gap:16px; }
    .muted { color:#6b7280; font-size:14px; }
    .answer { line-height:1.6; white-space:pre-wrap; }
    .pillbar { display:flex; flex-wrap:wrap; gap:8px; margin:12px 0 0; }
    .pill { background:#efe2d4; color:#6d4c36; border:1px solid #dbc2ae; border-radius:999px; padding:6px 10px; font-size:13px; }
    .chunk { border-top:1px solid var(--line); padding-top:12px; margin-top:12px; }
    .chunk h3 { margin:0 0 8px; font-size:15px; }
    textarea,input,button,select { width:100%; box-sizing:border-box; font:inherit; }
    textarea,input,select { border:1px solid var(--line); border-radius:10px; padding:12px; background:#fff; }
    button { background:var(--accent); color:#fff; border:none; border-radius:10px; padding:12px 16px; cursor:pointer; }
    pre { white-space:pre-wrap; word-break:break-word; background:#fcfaf5; border:1px solid var(--line); border-radius:10px; padding:12px; max-height:420px; overflow:auto; }
    ul { padding-left:20px; }
    @media (max-width: 820px) { .grid { grid-template-columns:1fr; } }
  </style>
</head>
<body>
  <main>
    <div class="card" style="margin-bottom:20px">
      <h1>Construction Estimating Agent</h1>
      <p>Grounded question answering over bid tabulation data and project PDFs, with explicit analysis tools.</p>
    </div>
    <div class="grid">
      <section class="card">
        <h2>Ask</h2>
        <textarea id="question" rows="6">What are the top 5 most expensive bid items?</textarea>
        <div style="height:12px"></div>
        <button id="askButton">Run Agent</button>
        <div style="height:16px"></div>
        <div class="stack">
          <div class="card" style="padding:16px">
            <div class="muted">Answer</div>
            <div id="answerText" class="answer">Waiting for a question.</div>
            <div id="metaPills" class="pillbar"></div>
          </div>
          <div class="card" style="padding:16px">
            <div class="muted">Citations</div>
            <ul id="citationsList"><li>None yet.</li></ul>
          </div>
          <div class="card" style="padding:16px">
            <div class="muted">Retrieved Context</div>
            <div id="chunksPanel">No retrieved chunks yet.</div>
          </div>
          <div class="card" style="padding:16px">
            <div class="muted">Raw JSON</div>
            <pre id="answer">Waiting for a question.</pre>
          </div>
        </div>
      </section>
      <aside class="card">
        <h2>Loaded Documents</h2>
        <ul>
          {{range .documents}}
          <li>{{.Name}} ({{.Type}} / {{.Source}})</li>
          {{end}}
        </ul>
        <hr style="border:none;border-top:1px solid var(--line);margin:20px 0">
        <h2>Upload</h2>
        <input id="uploadFile" type="file">
        <div style="height:12px"></div>
        <button id="uploadButton">Upload File</button>
      </aside>
    </div>
  </main>
  <script>
    function escapeHtml(value) {
      return String(value)
        .replaceAll('&', '&amp;')
        .replaceAll('<', '&lt;')
        .replaceAll('>', '&gt;')
        .replaceAll('"', '&quot;')
        .replaceAll("'", '&#39;');
    }

    function renderAskResponse(data) {
      document.getElementById('answer').textContent = JSON.stringify(data, null, 2);
      document.getElementById('answerText').textContent = data.answer || 'No answer.';

      const pills = [];
      if (data.tool) pills.push('Tool: ' + data.tool);
      if (data.route && data.route.source_hint) pills.push('Source: ' + data.route.source_hint);
      if (data.route && Array.isArray(data.route.tools) && data.route.tools.length) {
        pills.push('Route: ' + data.route.tools.join(', '));
      }
      const meta = document.getElementById('metaPills');
      meta.innerHTML = pills.map(pill => '<span class="pill">' + escapeHtml(pill) + '</span>').join('');

      const citations = document.getElementById('citationsList');
      if (Array.isArray(data.citations) && data.citations.length) {
        citations.innerHTML = data.citations.map(item => '<li>' + escapeHtml(item) + '</li>').join('');
      } else {
        citations.innerHTML = '<li>None.</li>';
      }

      const chunksPanel = document.getElementById('chunksPanel');
      if (Array.isArray(data.chunks) && data.chunks.length) {
        chunksPanel.innerHTML = data.chunks.map(chunk => {
          const title = chunk.source + ' pp.' + chunk.page_start + '-' + chunk.page_end;
          return '<div class="chunk">' +
            '<h3>' + escapeHtml(title) + '</h3>' +
            '<div class="answer">' + escapeHtml(chunk.text || '') + '</div>' +
          '</div>';
        }).join('');
      } else {
        chunksPanel.textContent = 'No retrieved chunks.';
      }
    }

    document.getElementById('askButton').addEventListener('click', async function () {
      const question = document.getElementById('question').value;
      document.getElementById('answer').textContent = 'Running...';
      document.getElementById('answerText').textContent = 'Running...';
      document.getElementById('chunksPanel').textContent = 'Running...';
      document.getElementById('citationsList').innerHTML = '<li>Running...</li>';
      document.getElementById('metaPills').innerHTML = '';
      const response = await fetch('/api/ask', {
        method: 'POST',
        headers: {'Content-Type':'application/json'},
        body: JSON.stringify({question})
      });
      const data = await response.json();
      renderAskResponse(data);
    });
    document.getElementById('uploadButton').addEventListener('click', async function () {
      const input = document.getElementById('uploadFile');
      if (!input.files.length) return;
      const form = new FormData();
      form.append('file', input.files[0]);
      const response = await fetch('/api/upload', { method: 'POST', body: form });
      const data = await response.json();
      document.getElementById('answer').textContent = JSON.stringify(data, null, 2);
      document.getElementById('answerText').textContent = data.status ? (data.status + ': ' + data.path) : 'Upload complete.';
      location.reload();
    });
  </script>
</body>
</html>`
