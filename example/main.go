package main

import (
	"context"
	_ "embed"
	"log"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema\.sql
var schema []byte

type config struct {
	PostgresDSN     string `env:"SIM_POSTGRES_DSN"`
	TransactionRate uint16 `env:"SIM_TRANSACTION_RATE"`
}

func main() {
	ctx := context.Background()
	cfg := config{
		PostgresDSN:     "postgresql://postgres:postgres@postgres:5432/postgres?sslmode=disable",
		TransactionRate: 1000,
	}
	if err := env.Parse(&cfg); err != nil {
		panic(err)
	}
	dbpool := dbMustConnect(ctx, cfg.PostgresDSN)
	defer dbpool.Close()
	mustCreateSchema(ctx, dbpool)
	// Run simulation
	// Print results to stdout
}

func mustCreateSchema(ctx context.Context, dbpool *pgxpool.Pool) {
	_, err := dbpool.Exec(ctx, string(schema))
	if err != nil {
		log.Fatalf("Unable to create schema: %v\n", err)
	}
	log.Println(`Schema created.`)
}

func dbMustConnect(ctx context.Context, dsn string) (dbpool *pgxpool.Pool) {
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
	return
}
