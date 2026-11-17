// Command seed creates the first admin from environment variables.
//
//	SEED_ADMIN_EMAIL, SEED_ADMIN_NAME, SEED_ADMIN_PASSWORD
//
// It is idempotent: if the email already exists nothing is changed.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"

	"github.com/saianikethreddyp/thandra-api/internal/auth"
	"github.com/saianikethreddyp/thandra-api/internal/platform/database"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
}

func run() error {
	_ = godotenv.Load()

	url := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	email := strings.TrimSpace(os.Getenv("SEED_ADMIN_EMAIL"))
	name := strings.TrimSpace(os.Getenv("SEED_ADMIN_NAME"))
	password := os.Getenv("SEED_ADMIN_PASSWORD")
	if url == "" || email == "" || password == "" {
		return fmt.Errorf("DATABASE_URL, SEED_ADMIN_EMAIL and SEED_ADMIN_PASSWORD are required")
	}
	if name == "" {
		name = "Admin"
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	pool, err := database.NewPool(ctx, url)
	if err != nil {
		return err
	}
	defer pool.Close()

	// The token manager is unused by seeding; the service only needs the repo.
	svc := auth.NewService(auth.NewPgRepository(pool), auth.NewTokenManager(strings.Repeat("x", 32), time.Minute), time.Hour)
	created, err := svc.SeedAdmin(ctx, email, name, password)
	if err != nil {
		return err
	}
	if created {
		fmt.Printf("created admin %s\n", strings.ToLower(email))
	} else {
		fmt.Printf("admin %s already exists; nothing changed\n", strings.ToLower(email))
	}
	return nil
}
