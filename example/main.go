package main

import (
	"context"
	_ "embed"
	"log"
	"net/http"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema\.sql
var schema []byte

type config struct {
	PostgresDSN string        `env:"SIM_POSTGRES_DSN"`
	RTT         time.Duration `env:"SIM_RTT"`
	Latency     time.Duration `env:"SIM_LATENCY"`
}

func main() {
	ctx := context.Background()
	cfg := config{
		PostgresDSN: "postgresql://postgres:postgres@postgres:5432/postgres?sslmode=disable",
		RTT:         1 * time.Millisecond,
		Latency:     250 * time.Millisecond,
	}
	if err := env.Parse(&cfg); err != nil {
		panic(err)
	}
	dbpool := mustConnect(ctx, cfg.PostgresDSN)
	defer dbpool.Close()
	s := &http.Server{
		Addr:    ":8000",
		Handler: newHandler(dbpool, cfg),
	}
	log.Printf("Starting server on %s\n", s.Addr)
	if err := s.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
	log.Printf("Done.")
}

func mustConnect(ctx context.Context, dsn string) (dbpool *pgxpool.Pool) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	dbpool, err := pgxpool.New(ctx, dsn)
	var greeting string
	for range 10 {
		err = dbpool.QueryRow(context.Background(), "select '1'").Scan(&greeting)
		if err == nil {
			break
		}
		log.Println(err)
		time.Sleep(time.Second)
	}
	if err != nil {
		log.Fatalf("Unable to connect to database: %v\n", err)
	}
	log.Println(`Datbase connection established.`)
	_, err = dbpool.Exec(ctx, string(schema))
	if err != nil {
		log.Fatalf("Unable to create schema: %v\n", err)
	}
	log.Println(`Schema created.`)
	return
}
