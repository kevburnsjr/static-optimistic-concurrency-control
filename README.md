# Guided Optimistic Concurrency Control

This repository contains a proof of concept and simulation test suite for a novel refinement of
**Optimistic Concurrency Control** (OCC) designed to optimize throughput in high contention OCC workloads.

## Overview

In traditional OCC, a transaction is executed and submitted for commit with the optimistic assumption that the process
executing the transaction (the "client") is the only process operating on a set of records (the "read set") for the
duration of the transaction. At commit time, the database validates whether any records in the read set were modified
by another process during the transaction. If the read set is clean, then the transaction commits successfully. If the
read set is dirty, the commit fails.

After a failure, the client typically sleeps for some expontial backoff interval and then retries the transaction. While
this retry strategy is the simplest and most common, it is not the only one. Other retry strategies include 
**Hybrid Concurrency Control** [^1] where the client eschews the optimistic approach and switches to a pessimistic
concurrency control scheme on retry (ie. interactively acquiring an exclusive lock on every record in the read set).

**Guided Optimistic Concurrency Control** is a novel [^2] retry strategy where upon rejecting a propsoed commit, the
database responds with not just a rejection but also a fresh copy of all records in the read set that failed validation.
This gives the client an opportunity to update its write-back cache and re-execute the transaction with the optimistic
assumption that the transaction can be downgraded to a *static* data access scheme on retry.

If no new keys are accessed on retry then all reads can be served from the clinet's cache meaning that the number of
network round trips between the client and the database during the course of a transaction on retry is reduced from
`O(n)` to `O(1)`. In cases where network round trip time during *dynamic* data access transactions is a primary factor
in systemic transaction throughput, a system that successfully reduces the number of network round trips for an
optimistic concurrency transaction to its theoretical minimum of `1` (a key property of the static data access scheme)
should see a noticable effect on latency and throughput for high contention workloads. 

This strategy may reduce total system tail latencies since the retry can be executed immediately without the need for
exponential backoff. It may also produce noticable effects on total database network traffic since the database need
only return dirty records on retry rather than re-fetching the entire read set.

## Key Concepts

* OCC **Data Access Scheme** (static / dynamic) [^3]
* OCC **Commit Scheme** (silent / broadcast) [^3]

## Adjacent Work

* **Hybrid Concurrency Control** [^1]

[^1]: [Analysis of Some Optimistic Concurrency Control Schemes Based on Certification](https://dl.acm.org/doi/10.1145/317795.317824) (1985)
[^2]: Novel *as far as we know*. If you know of any prior art that describes a similar strategy please tell us.
[^3]: [Analysis of Hybrid Concurrency Control Schemes for a High Data Contention Environment](https://dl.acm.org/doi/abs/10.1109/32.121754) (1992)
