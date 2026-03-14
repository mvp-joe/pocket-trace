package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	trace "pocket-trace"
)

func main() {
	trace.SetServiceName("example-app")
	trace.SetExporter(trace.NewHTTPExporter("http://localhost:7070"))
	defer trace.Shutdown(context.Background())

	ctx := context.Background()

	// Simulate a few requests.
	for i := range 5 {
		handleRequest(ctx, fmt.Sprintf("/api/users/%d", i+1))
		time.Sleep(100 * time.Millisecond)
	}

	fmt.Println("\nDone. Check traces at http://localhost:7070")
}

func handleRequest(ctx context.Context, path string) {
	span, ctx := trace.Start(ctx, "handle-request", "http.path", path, "http.method", "GET")
	defer span.End()

	// Auth check.
	authenticate(ctx)

	// Load data.
	data, err := loadFromDB(ctx, path)
	if err != nil {
		span.Event("error-response", "status", 500)
		return
	}

	// Render response.
	renderResponse(ctx, data)
	span.Event("response-sent", "status", 200, "bytes", rand.IntN(4096)+256)
}

func authenticate(ctx context.Context) {
	span, _ := trace.Start(ctx, "authenticate", "method", "bearer")
	defer span.End()

	time.Sleep(time.Duration(rand.IntN(5)+1) * time.Millisecond)
	span.Event("token-validated")
}

func loadFromDB(ctx context.Context, path string) (string, error) {
	span, ctx := trace.Start(ctx, "db-query", "table", "users")
	var err error
	defer span.EndErr(&err)

	// Simulate cache check.
	cached := rand.IntN(3) == 0
	span.Event("cache-check", "hit", cached)

	if cached {
		return "cached-data", nil
	}

	// Simulate a slow query.
	queryTime := time.Duration(rand.IntN(50)+10) * time.Millisecond
	time.Sleep(queryTime)
	span.Event("query-complete", "rows", rand.IntN(100)+1, "duration_ms", queryTime.Milliseconds())

	// Simulate occasional errors.
	if path == "/api/users/3" {
		err = errors.New("connection reset by peer")
		return "", err
	}

	return "db-data", nil
}

func renderResponse(ctx context.Context, data string) {
	span, _ := trace.Start(ctx, "render-response", "format", "json")
	defer span.End()

	time.Sleep(time.Duration(rand.IntN(3)+1) * time.Millisecond)
}
