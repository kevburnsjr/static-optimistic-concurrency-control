package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	static      = true
	pessimistic = false
)

func newHandler(dbpool *pgxpool.Pool, cfg config) http.Handler {
	return &handler{
		dbpool: dbpool,
		cfg:    cfg,
	}
}

type handler struct {
	dbpool *pgxpool.Pool
	cfg    config
}

type apiRequest struct {
	Path string `json:"path"`
	Args args   `json:"args"`
}

type args struct {
	ID      int `json:"id"`
	Amount  int `json:"amount"`
	Count   int `json:"count"`
	From    int `json:"from_id"`
	Initial int `json:"initial"`
	Start   int `json:"start"`
	To      int `json:"to_id"`
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	time.Sleep(h.cfg.Latency / 2)
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	switch r.URL.Path {
	case "/api/query":
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var req apiRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		switch req.Path {
		case "accounts:get_account":
			//
		default:
			http.NotFound(w, r)
		}
	case "/api/mutation":
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var req apiRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		switch req.Path {
		case "seed:clear_accounts":
			h.clear(w, r)
		case "seed:seed_range":
			h.seed(w, r, req.Args)
		case "transfer:transfer":
			h.transfer(w, r, req.Args)
		default:
			http.NotFound(w, r)
		}
	default:
		http.NotFound(w, r)
	}
}

func (h *handler) clear(w http.ResponseWriter, r *http.Request) {
	log.Printf("Clearing accounts\n")
	var n int
	if err := h.dbpool.QueryRow(r.Context(),
		`SELECT count(*) FROM accounts`,
	).Scan(&n); err != nil {
		log.Printf("Error counting accounts: %v\n", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := h.dbpool.Exec(r.Context(),
		`DELETE FROM accounts`,
	); err != nil {
		log.Printf("Error clearing accounts: %v\n", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	time.Sleep(h.cfg.Latency / 2)
	w.Header().Set("Content-Type", "application/json")
	w.Write(fmt.Appendf(nil, `{"status": "success", "value": %d}`, n))
}

func (h *handler) seed(w http.ResponseWriter, r *http.Request, req args) {
	log.Printf("Seeding accounts %d-%d with initial balance %d\n", req.Start, req.Start+req.Count, req.Initial)
	for i := req.Start; i < req.Start+req.Count; i++ {
		// time.Sleep(h.cfg.RTT / 2)
		if _, err := h.dbpool.Exec(r.Context(),
			`INSERT INTO ACCOUNTS (id, version, balance) VALUES ($1, 1, $2);`, i, req.Initial,
		); err != nil {
			log.Printf("Error seeding account: %v\n", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// time.Sleep(h.cfg.RTT / 2)
	}
	log.Printf("Done seeding accounts %d-%d\n", req.Start, req.Start+req.Count)
	time.Sleep(h.cfg.Latency / 2)
	w.Header().Set("Content-Type", "application/json")
	w.Write(fmt.Appendf(nil, `{"status": "success"}`))
}

type account struct {
	ID      int
	Balance int
	Version int
}

func (h *handler) transfer(w http.ResponseWriter, r *http.Request, req args) {
	ctx := r.Context()
	var err error
	var from, to account
	// 3 outer attempts
	for i := range 3 {
		time.Sleep(time.Duration(i) * 10 * time.Millisecond)
		if from, err = h.findAccount(ctx, req.From); err != nil {
			log.Printf("Error finding from account: %v\n", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if to, err = h.findAccount(ctx, req.To); err != nil {
			log.Printf("Error finding to account: %v\n", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if from.Balance < req.Amount {
			time.Sleep(h.cfg.Latency / 2)
			w.Write(fmt.Appendf(nil, `{"status": "success"}`))
			return
		}
		time.Sleep(h.cfg.RTT / 2)
		batch := &pgx.Batch{}
		// batch.Queue(`UPDATE accounts SET balance = balance - $1, version = $2 WHERE id = $3`, req.Amount, from.Version, from.ID)
		// batch.Queue(`UPDATE accounts SET balance = balance + $1, version = $2 WHERE id = $3`, req.Amount, to.Version, to.ID)
		batch.Queue(fmt.Sprintf(`UPDATE accounts SET balance = balance - %d, version = %d WHERE id = %d`, req.Amount, from.Version, from.ID))
		batch.Queue(fmt.Sprintf(`UPDATE accounts SET balance = balance + %d, version = %d WHERE id = %d`, req.Amount, to.Version, to.ID))
		err = h.dbpool.SendBatch(ctx, batch).Close()
		time.Sleep(h.cfg.RTT / 2)
		if err == nil {
			time.Sleep(h.cfg.Latency / 2)
			w.Write(fmt.Appendf(nil, `{"status": "success"}`))
			return
		}
		if !strings.Contains(err.Error(), "VERSION_CONFLICT") && !strings.Contains(err.Error(), "deadlock detected") {
			log.Printf("Error closing batch: %v\n", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// 2 inner attempts
		if static {
			for range 2 {
				if !pessimistic {
					h.refreshAccountsStatic(ctx, &from, &to)
					if from.Balance < req.Amount {
						time.Sleep(h.cfg.Latency / 2)
						w.Write(fmt.Appendf(nil, `{"status": "success"}`))
						return
					}
					time.Sleep(h.cfg.RTT / 2)
					batch := &pgx.Batch{}
					// batch.Queue(`UPDATE accounts SET balance = balance - $1, version = $2 WHERE id = $3`, req.Amount, from.Version, from.ID)
					// batch.Queue(`UPDATE accounts SET balance = balance + $1, version = $2 WHERE id = $3`, req.Amount, to.Version, to.ID)
					batch.Queue(fmt.Sprintf(`UPDATE accounts SET balance = balance - %d, version = %d WHERE id = %d`, req.Amount, from.Version, from.ID))
					batch.Queue(fmt.Sprintf(`UPDATE accounts SET balance = balance + %d, version = %d WHERE id = %d`, req.Amount, to.Version, to.ID))
					err = h.dbpool.SendBatch(ctx, batch).Close()
					time.Sleep(h.cfg.RTT / 2)
					if err == nil {
						time.Sleep(h.cfg.Latency / 2)
						w.Write(fmt.Appendf(nil, `{"status": "success"}`))
						return
					}
					if !strings.Contains(err.Error(), "VERSION_CONFLICT") && !strings.Contains(err.Error(), "deadlock detected") {
						log.Printf("Error closing batch: %v\n", err)
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
				} else {
					txn, err := h.dbpool.Begin(ctx)
					if err != nil {
						txn.Rollback(ctx)
						log.Printf("Error beginning transaction: %v\n", err)
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					err = h.refreshAccountsPessimistic(ctx, txn, &from, &to)
					if err != nil {
						txn.Rollback(ctx)
						log.Printf("Error refreshing accounts pessimistically: %v\n", err)
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					if from.Balance < req.Amount {
						txn.Rollback(ctx)
						time.Sleep(h.cfg.Latency / 2)
						w.Write(fmt.Appendf(nil, `{"status": "success"}`))
						return
					}
					time.Sleep(h.cfg.RTT / 2)
					_, err = txn.Exec(ctx, fmt.Sprintf(`UPDATE accounts SET balance = balance - %d, version = %d WHERE id = %d`, req.Amount, from.Version, from.ID))
					if err != nil {
						txn.Rollback(ctx)
						continue
					}
					_, err = txn.Exec(ctx, fmt.Sprintf(`UPDATE accounts SET balance = balance + %d, version = %d WHERE id = %d`, req.Amount, to.Version, to.ID))
					if err != nil {
						txn.Rollback(ctx)
						continue
					}
					// batch := &pgx.Batch{}
					// batch.Queue(`UPDATE accounts SET balance = balance - $1, version = $2 WHERE id = $3`, req.Amount, from.Version, from.ID)
					// batch.Queue(`UPDATE accounts SET balance = balance + $1, version = $2 WHERE id = $3`, req.Amount, to.Version, to.ID)
					// batch.Queue(fmt.Sprintf(`UPDATE accounts SET balance = balance - %d, version = %d WHERE id = %d`, req.Amount, from.Version, from.ID))
					// batch.Queue(fmt.Sprintf(`UPDATE accounts SET balance = balance + %d, version = %d WHERE id = %d`, req.Amount, to.Version, to.ID))
					// batch.Queue(`COMMIT`)
					// err = txn.SendBatch(ctx, batch).Close()
					err = txn.Commit(ctx)
					time.Sleep(h.cfg.RTT / 2)
					if err == nil {
						txn.Rollback(ctx)
						time.Sleep(h.cfg.Latency / 2)
						w.Write(fmt.Appendf(nil, `{"status": "success"}`))
						return
					}
					if !strings.Contains(err.Error(), "VERSION_CONFLICT") && !strings.Contains(err.Error(), "deadlock detected") {
						txn.Rollback(ctx)
						log.Printf("Error closing batch: %v\n", err)
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
				}
			}
		}
	}
	time.Sleep(h.cfg.Latency / 2)
	w.WriteHeader(503)
	w.Write(fmt.Appendf(nil, `{"status": "error", "errorMessage": "%s"}`, strconv.Quote(conflictErrMsg)))
}

const conflictErrMsg = `Documents read from or written to the "accounts" table changed while this mutation was ` +
	`being run and on every subsequent retry. Another call to this mutation changed the document with ID ` +
	`"j57cgswzpa21j2n0zdes37kgah86b9mc". See https://docs.convex.dev/error#1`

func (h *handler) findAccount(ctx context.Context, id int) (acct account, err error) {
	time.Sleep(h.cfg.RTT / 2)
	rows, err := h.dbpool.Query(ctx, "select * from accounts where id=$1", id)
	if err != nil {
		return
	}
	acct, err = pgx.CollectOneRow(rows, pgx.RowToStructByName[account])
	time.Sleep(h.cfg.RTT / 2)
	return
}

func (h *handler) refreshAccountsStatic(ctx context.Context, from, to *account) (err error) {
	time.Sleep(h.cfg.RTT / 2)
	defer time.Sleep(h.cfg.RTT / 2)
	// rows, err := h.dbpool.Query(ctx, `SELECT * FROM accounts WHERE (id = $1 AND version > $2)  OR (id = $3 AND version > $4)`, from.ID, from.Version, to.ID, to.Version)
	rows, err := h.dbpool.Query(ctx, fmt.Sprintf(`SELECT * FROM accounts WHERE (id = %d AND version > %d)  OR (id = %d AND version > %d)`, from.ID, from.Version, to.ID, to.Version))
	if err != nil {
		return
	}
	accounts, err := pgx.CollectRows(rows, pgx.RowToStructByName[account])
	if err != nil {
		return
	}
	for _, acct := range accounts {
		switch acct.ID {
		case from.ID:
			*from = acct
		case to.ID:
			*to = acct
		}
	}
	return
}

func (h *handler) refreshAccountsPessimistic(ctx context.Context, txn pgx.Tx, from, to *account) (err error) {
	time.Sleep(h.cfg.RTT / 2)
	defer time.Sleep(h.cfg.RTT / 2)
	// rows, err := txn.Query(ctx, `SELECT * FROM accounts WHERE (id = $1 AND version > $2) OR (id = $3 AND version > $4) FOR UPDATE`, from.ID, from.Version, to.ID, to.Version)
	rows, err := txn.Query(ctx, fmt.Sprintf(`SELECT * FROM accounts WHERE (id = %d AND version > %d)  OR (id = %d AND version > %d) FOR UPDATE`, from.ID, from.Version, to.ID, to.Version))
	if err != nil {
		return
	}
	accounts, err := pgx.CollectRows(rows, pgx.RowToStructByName[account])
	if err != nil {
		return
	}
	for _, acct := range accounts {
		switch acct.ID {
		case from.ID:
			*from = acct
		case to.ID:
			*to = acct
		}
	}
	return
}
