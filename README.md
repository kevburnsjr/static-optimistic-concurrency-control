# Static Optimistic Concurrency Control

This repository contains a proof of concept and simulation test suite for a novel refinement of
**Optimistic Concurrency Control** (OCC) designed to optimize throughput in high contention OCC workloads.

## Overview

In traditional OCC, a transaction is executed and submitted for commit with an assumption of isolation; that the process
executing the transaction (the "client") is the only process operating on the set of records required to execute the
transaction (the "read set") at a chosen isolation level for the duration of the transaction. At commit time, the
database validates whether any records in the read set were modified by another process during the transaction. If the
read set is unmodified, then the transaction commits successfully because the assumption was correct. But if any record
in the read set has been modified since it was read, the commit must fail in order to preserve consistency.

After a failed commit, the client typically sleeps for some exponential backoff interval and then retries the
transaction. While this retry strategy is the simplest and most common, it is not the only one. Other retry strategies
include **Hybrid Concurrency Control** [^1] where the client eschews the optimistic approach and switches to a
pessimistic concurrency control strategy on retry (ie. interactively acquiring an exclusive lock on every record in the
read set).

**Static Optimistic Concurrency Control** is a novel OCC retry strategy where the client caches the read set during the
first execution, updates only the stale values upon failure (capturing a fresh partial snapshot of the database) and
then immediately retries the transaction with the optimistic assumption that the read set will not change across
executions. This allows all stale keys in the write-back cache to be updated to their latest version simultaneously,
optimistically downgrading the transaction to a *static* data access scheme.

If no new keys are accessed on retry then all reads can be served from the client's cache meaning that the number of
network round trips between the client and the database during the course of a transaction on retry is reduced from
`O(n)` to `O(1)`. In cases where aggregate network round trip latency during *dynamic* data access transactions is
a primary limiting factor in systemic transaction throughput, a system that can successfully reduce the number of
network round trips for optimistic concurrency transaction retries to its theoretical minimum of `1` (a key property of
the *static* data access scheme) might see a significant improvement in latency and throughput for high contention
workloads.

This strategy may reduce total system tail latencies since retries can be executed immediately without the need for
exponential backoff on conflict. It may also produce noticeable effects on total database network traffic since only
modified records need to be refreshed on each retry rather than re-fetching the entire read set.

## Key Concepts

* OCC **Data Access Scheme** (static / dynamic) [^2]

## Sequence Diagram

<img alt="OCC Conflict Resolution" src="occ-conflict-resolution.png"/>

Reduced network roundtrip count on static retry minimizes opportunity for conflict.

## Example

Postgres schema for basic OCC write protection at the `Read Committed` isolation level.

```sql
CREATE TABLE kvstore (
    "key" VARCHAR(255) PRIMARY KEY,
    "version" INTEGER NOT NULL,
    "data" JSON NOT NULL
);

CREATE OR REPLACE FUNCTION occ_write_check()
  RETURNS TRIGGER AS $$
  BEGIN
    IF (NEW.version != OLD.version) THEN
      RAISE EXCEPTION 'VERSION_CONFLICT' USING ERRCODE='OC000';
    ELSE
      NEW.version := NEW.version + 1;
    END IF;
    RETURN NEW;
  END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE TRIGGER tg_occ_write_check BEFORE UPDATE ON kvstore
  FOR EACH ROW EXECUTE PROCEDURE occ_write_check();
```

Usage

```sql
INSERT INTO kvstore ("key", "version", "data") VALUES ('foo', 1, '{"bar": 1}');
-- INSERT 0 1

UPDATE kvstore SET "version" = 1, "data" = '{"bar": 2}' WHERE "key" = 'foo';
-- UPDATE 1

UPDATE kvstore SET "version" = 1, "data" = '{"bar": 3}' WHERE "key" = 'foo';
-- ERROR: VERSION_CONFLICT
```

## Simulation

We're going to write an application in Go to exercise this OCC schema with tunable dimensions to compare the two
different retry strategies (traditional / static).

### Tunable Dimensions

* Key Space Size (default 100)
* DB Roundtrip Latency (default 1ms)
* Transaction Rate (default 1000/s)
* Read Set Size Min (default 1)
* Read Set Size Max (default 10)
* Read Set Size Distribution (default zipfian) (options: linear, static)
* Data Size Min (default 100b)
* Data Size Max (default 100kb)
* Data Size Distribution (default zipfian) (options: linear, static)
* Isolation Level (default ReadCommitted) (options: Serializable)

### Metrics

* Conflict Rate
* Average Retries
* Retry Count Burndown
* Latency Quantiles
* Active Transaction Count

[^1]: [Analysis of Hybrid Concurrency Control Schemes for a High Data Contention Environment](https://dl.acm.org/doi/abs/10.1109/32.121754) (1992)
[^2]: [Analysis of Some Optimistic Concurrency Control Schemes Based on Certification](https://dl.acm.org/doi/10.1145/317795.317824) (1985)
