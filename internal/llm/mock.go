package llm

import (
	"context"
	"fmt"
	"strings"
)

// MockProvider returns deterministic, realistic fake responses.
// It routes by scanning the system prompt for agent role keywords so each
// agent gets the correct shape of reply without hitting any real API.
type MockProvider struct {
	// CustomResponses can override the built-in defaults, keyed by role keyword.
	CustomResponses map[string]string
}

// NewMockProvider returns a MockProvider pre-loaded with realistic demo responses.
func NewMockProvider() *MockProvider {
	return &MockProvider{CustomResponses: defaultResponses()}
}

func (m *MockProvider) Name() string { return "mock" }

func (m *MockProvider) Generate(_ context.Context, req Request) (Response, error) {
	prompt := strings.ToLower(req.SystemPrompt)
	for keyword, body := range m.CustomResponses {
		if strings.Contains(prompt, keyword) {
			return Response{
				Content: body,
				Usage:   Usage{InputTokens: 120, OutputTokens: 350},
			}, nil
		}
	}
	// Fallback: echo the last user message
	last := ""
	if len(req.Messages) > 0 {
		last = req.Messages[len(req.Messages)-1].Content
	}
	return Response{Content: fmt.Sprintf("(mock) no route matched; user said: %s", last)}, nil
}

// defaultResponses maps routing keys to canned responses.
// Keys use the unique opening sentence of each agent's template so that
// partial keyword collisions (e.g. "reviewer" appearing in the coordinator
// template as "Reviewer feedback:") never cause mis-routing.
func defaultResponses() map[string]string {
	return map[string]string{

		// Coordinator produces a plain-text narrative summary
		"you are the coordinator agent": `Workflow progress report:

The task has been received and the pipeline is running as expected. The planner has decomposed the work into clear, dependency-ordered steps. The coder is implementing the solution based on that plan. The reviewer will inspect the output and flag any issues before the result is finalised. I will coordinate revision cycles as needed, up to the configured maximum, then surface the best available output.`,

		// Planner produces a JSON Plan
		"you are the planner agent": `{
  "summary": "Build a minimal Go HTTP server with a health endpoint, structured logging, and graceful shutdown",
  "steps": [
    {
      "id": 1,
      "title": "Initialise Go module",
      "description": "Create go.mod with module path and Go version requirement.",
      "depends_on": []
    },
    {
      "id": 2,
      "title": "Write HTTP handler",
      "description": "Implement GET /health returning 200 OK with a JSON body.",
      "depends_on": [1]
    },
    {
      "id": 3,
      "title": "Add structured logging",
      "description": "Use log/slog to emit startup and request log lines.",
      "depends_on": [2]
    },
    {
      "id": 4,
      "title": "Implement graceful shutdown",
      "description": "Catch SIGINT/SIGTERM and call http.Server.Shutdown with a timeout.",
      "depends_on": [2]
    },
    {
      "id": 5,
      "title": "Write unit tests",
      "description": "Test the health handler using net/http/httptest.",
      "depends_on": [2]
    }
  ],
  "dependencies": ["Go 1.21+"],
  "milestones": ["Module initialised", "Server responds on /health", "Tests pass", "Graceful shutdown verified"],
  "risks": ["Port 8080 may be in use in CI; parameterise via PORT env var"],
  "assumptions": ["Go toolchain is installed", "No external libraries are required"]
}`,

		// Coder produces a JSON Artifact
		"you are the coder agent": `{
  "summary": "Minimal Go HTTP server with /health endpoint, slog logging, and graceful shutdown",
  "revision": 1,
  "files": [
    {
      "path": "go.mod",
      "lang": "text",
      "content": "module example.com/server\n\ngo 1.21\n"
    },
    {
      "path": "main.go",
      "lang": "go",
      "content": "package main\n\nimport (\n\t\"context\"\n\t\"encoding/json\"\n\t\"log/slog\"\n\t\"net/http\"\n\t\"os\"\n\t\"os/signal\"\n\t\"syscall\"\n\t\"time\"\n)\n\nfunc healthHandler(w http.ResponseWriter, r *http.Request) {\n\tw.Header().Set(\"Content-Type\", \"application/json\")\n\tjson.NewEncoder(w).Encode(map[string]string{\"status\": \"ok\"})\n}\n\nfunc main() {\n\tlogger := slog.New(slog.NewJSONHandler(os.Stdout, nil))\n\tslog.SetDefault(logger)\n\n\tport := os.Getenv(\"PORT\")\n\tif port == \"\" {\n\t\tport = \"8080\"\n\t}\n\n\tmux := http.NewServeMux()\n\tmux.HandleFunc(\"/health\", healthHandler)\n\n\tsrv := &http.Server{\n\t\tAddr:         \":\" + port,\n\t\tHandler:      mux,\n\t\tReadTimeout:  5 * time.Second,\n\t\tWriteTimeout: 10 * time.Second,\n\t}\n\n\tgo func() {\n\t\tslog.Info(\"server starting\", \"port\", port)\n\t\tif err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {\n\t\t\tslog.Error(\"server error\", \"err\", err)\n\t\t\tos.Exit(1)\n\t\t}\n\t}()\n\n\tquit := make(chan os.Signal, 1)\n\tsignal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)\n\t<-quit\n\n\tctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)\n\tdefer cancel()\n\tif err := srv.Shutdown(ctx); err != nil {\n\t\tslog.Error(\"shutdown error\", \"err\", err)\n\t}\n\tslog.Info(\"server stopped\")\n}\n"
    },
    {
      "path": "main_test.go",
      "lang": "go",
      "content": "package main\n\nimport (\n\t\"net/http\"\n\t\"net/http/httptest\"\n\t\"testing\"\n)\n\nfunc TestHealthHandler(t *testing.T) {\n\treq := httptest.NewRequest(http.MethodGet, \"/health\", nil)\n\tw := httptest.NewRecorder()\n\thealthHandler(w, req)\n\n\tif w.Code != http.StatusOK {\n\t\tt.Fatalf(\"expected 200, got %d\", w.Code)\n\t}\n\tif ct := w.Header().Get(\"Content-Type\"); ct != \"application/json\" {\n\t\tt.Fatalf(\"expected application/json, got %s\", ct)\n\t}\n}\n"
    }
  ]
}`,

		// Reviewer produces a JSON Review
		"you are the reviewer agent": `{
  "approved": true,
  "score": 9,
  "feedback": "The implementation is clean, idiomatic, and complete. It covers all plan steps including graceful shutdown and structured logging. Tests are present and focused.",
  "issues": [
    {
      "severity": "minor",
      "file": "main.go",
      "description": "The server error branch calls os.Exit(1) directly, which bypasses deferred cleanup.",
      "suggestion": "Send the error to a channel and handle it in main so deferred calls run."
    }
  ],
  "revision": 1
}`,
	}
}
