// Command migrate applies database migrations.
//
// Usage: migrate [up|down|status]
//
// It prefers DATABASE_URL (Neon direct connection) because schema changes
// should not go through the connection pooler.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"
	"github.com/pressly/goose/v3"

	"github.com/saianikethreddyp/thandra-api/db"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
}

func run() error {
	_ = godotenv.Load()

	url := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if url == "" {
		url = strings.TrimSpace(os.Getenv("DATABASE_URL_POOLED"))
	}
	if url == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}

	cmd := "up"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	conn, err := sql.Open("pgx", url)
	if err != nil {
		return err
	}
	defer conn.Close()

	goose.SetBaseFS(db.Migrations)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}

	ctx := context.Background()
	switch cmd {
	case "up":
		return goose.UpContext(ctx, conn, "migrations")
	case "down":
		return goose.DownContext(ctx, conn, "migrations")
	case "status":
		return goose.StatusContext(ctx, conn, "migrations")
	default:
		return fmt.Errorf("unknown command %q (use up, down or status)", cmd)
	}
}
