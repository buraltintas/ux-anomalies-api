package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"github.com/pressly/goose/v3"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	_ = godotenv.Load(".env")
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	migrationsURL := strings.TrimSpace(os.Getenv("MIGRATIONS_DATABASE_URL"))
	if migrationsURL == "" {
		migrationsURL = databaseURL
	}
	if migrationsURL == "" {
		log.Fatal("DATABASE_URL or MIGRATIONS_DATABASE_URL must be set")
	}
	if len(os.Args) < 2 {
		log.Fatal("usage: go run ./cmd/migrate [up|down]")
	}
	cmd := os.Args[1]
	if cmd != "up" && cmd != "down" {
		log.Fatalf("unsupported command: %s", cmd)
	}

	db, err := sql.Open("pgx", migrationsURL)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		log.Fatal(err)
	}

	if err := goose.SetDialect("postgres"); err != nil {
		log.Fatal(err)
	}
	switch cmd {
	case "up":
		if err := goose.Up(db, "migrations"); err != nil {
			log.Fatal(err)
		}
	case "down":
		if err := goose.Down(db, "migrations"); err != nil {
			log.Fatal(err)
		}
	}
	fmt.Println("ok")
}
