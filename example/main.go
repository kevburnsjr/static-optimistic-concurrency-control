package main

import (
	"context"
	_ "embed"
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	offset  = &atomic.Int64{}
	active  = &atomic.Int64{}
	randSrc = &rand.ChaCha8{}
)

//go:embed schema\.sql
var schema []byte

type config struct {
	Duration         time.Duration `env:"SIM_DURATION"`
	ValSize          uint32        `env:"SIM_VAL_SIZE"`
	KeySpaceSize     uint16        `env:"SIM_KEY_SPACE_SIZE"`
	PostgresDSN      string        `env:"SIM_POSTGRES_DSN"`
	ReadSetSize      uint16        `env:"SIM_READ_SET_SIZE"`
	RTT              time.Duration `env:"SIM_RTT"`
	TxnBatchInterval time.Duration `env:"SIM_TXN_BATCH_INTERVAL"`
	TxnRate          uint16        `env:"SIM_TXN_RATE"`
	WritePercent     uint8         `env:"SIM_WRITE_PERCENT"`
	Seed             uint64        `env:"SIM_SEED"`
}

func main() {
	ctx := context.Background()
	cfg := config{
		Duration:     time.Minute,
		ValSize:      100,
		KeySpaceSize: 100,
		PostgresDSN:  "postgresql://postgres:postgres@postgres:5432/postgres?sslmode=disable",
		ReadSetSize:  10,
		RTT:          1 * time.Millisecond,
		TxnRate:      100,
		WritePercent: 10,
		Seed:         uint64(time.Now().UnixNano()),
	}
	if err := env.Parse(&cfg); err != nil {
		panic(err)
	}
	if cfg.TxnBatchInterval == 0 {
		cfg.TxnBatchInterval = time.Second / time.Duration(cfg.TxnRate)
	}
	var seedBytes [32]byte
	binary.LittleEndian.PutUint64(seedBytes[24:], cfg.Seed)
	randSrc.Seed(seedBytes)
	offset.Store(int64(cfg.Seed))
	dbpool := mustConnect(ctx, cfg.PostgresDSN)
	defer dbpool.Close()
	ctx, cancel := context.WithTimeout(ctx, cfg.Duration)
	defer cancel()
	log.Printf("Running simulation for %s with config: %+v\n", cfg.Duration, cfg)
	var wg sync.WaitGroup
	var t = time.NewTicker(cfg.TxnBatchInterval)
	var keys = make([]string, cfg.KeySpaceSize)
	var vals = make([][]byte, cfg.KeySpaceSize)
	for i := range keys {
		keys[i] = fmt.Sprintf("%08d", i)
		vals[i] = make([]byte, cfg.ValSize)
		randSrc.Read(vals[i])
	}
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			log.Println("Done.")
			return
		case <-t.C:
		}
		wg.Add(1)
		active.Add(1)
		o := next()
		r := int(randSrc.Uint64() / 2)
		wg.Go(func() {
			defer active.Add(-1)
			defer wg.Done()
			log.Printf(`TXN START %d %s %x`, o, keys[r%len(keys)], string(vals[r%len(vals)][:4]))
			reads := make([]string, cfg.ReadSetSize)
			for i := range reads {
				reads[i] = keys[(o+i)%len(keys)]
			}
			writes := make([]string, int(math.Ceil(float64(len(reads))*float64(cfg.WritePercent))))
			for i := range writes {
				writes[i] = fmt.Sprintf("%05d", i)
			}
			//
		})
	}
	// Run simulation
	// Print results to stdout
}

func next() int {
	return int(offset.Add(1))
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
