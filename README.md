# 🛡️ Go Concurrency & Immutable Double-Entry Ledger Engine
> **An ACID-compliant financial engine implementing double-entry accounting invariants, minor currency unit arithmetic, and pessimistic concurrency protection against race conditions.**

[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat&logo=go)](https://golang.org)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Architecture](https://img.shields.io/badge/Design-Append--Only%20Ledger-success)]()

---

## 📌 Architectural Overview

Financial and commerce systems fail not from complex logic, but from subtle violations of core accounting and concurrency laws:
1. **Floating-point rounding drift** (e.g., storing money as `float64`).
2. **Unbalanced state mutations** (e.g., executing `UPDATE balance = balance + x` without an immutable counter-entry).
3. **Check-Then-Act Race Conditions** (e.g., concurrent checkout or withdrawal requests reading balance simultaneously and double-spending funds).

This engine demonstrates a robust, zero-drift solution built in **Go** adhering to 4 strict architectural invariants:

```
                  ┌──────────────────────────────┐
                  │    HTTP / API Ingestion      │
                  └──────────────┬───────────────┘
                                 │
                 [ Pre-Flight Invariant Validation ]
                     Σ Debits == Σ Credits ?
                                 │
                                 ▼
             ┌────────────────────────────────────────┐
             │       ACID Transaction Boundary        │
             │                                        │
             │  1. Acquire Row / Engine Write Lock    │
             │  2. Compute Balance from Entries Table │
             │  3. Verify Solvency                    │
             │  4. Insert Immutable Ledger Entries    │
             │  5. Atomically Commit & Release Lock   │
             └───────────────────┬────────────────────┘
                                 │
                                 ▼
                    ┌────────────────────────┐
                    │ Immutable SQLite / SQL │
                    │ (Append-Only Journal)  │
                    └────────────────────────┘
```

---

## 🏛️ The Four Core Invariants

### 1. The Minor Units Law (Zero Floating-Point Drift)
* Binary floating-point types (`float32`, `float64`) cannot represent decimal fractions precisely (e.g. `0.1 + 0.2 = 0.30000000000000004`).
* **Rule:** Money is stored and manipulated strictly as positive 64-bit integers (`int64`) in the currency's smallest sub-unit ($10.00 \rightarrow `1000` cents, ₦1,000.00 \rightarrow `100000` kobo).

### 2. The Double-Entry Equilibrium ($\sum \text{Entries} = 0$)
* Money cannot be created or destroyed. Every transaction must be a balanced transfer between two or more accounts.
* **Rule:** An inbound deposit debits the physical bank clearing account and credits the customer's wallet. The sum of debits must mathematically equal the sum of credits before the database transaction is initiated.

### 3. Append-Only Immutability (Permanent Audit Ink)
* There is **no mutable `balance` column** on accounts.
* Account balances are derived dynamically via `SUM(Credits) - SUM(Debits)`.
* Updates and deletions are forbidden (`REVOKE UPDATE, DELETE ON entries`).
* Adjustments and refunds are handled exclusively through **compensating reversal transactions**, guaranteeing an indisputable chronological audit history.

### 4. Concurrency Protection (Preventing the Check-Then-Act Trap)
* A high-concurrency race condition occurs when multiple workers inspect a balance at the same microsecond and both execute deductions before either writes to disk.
* **Defense:** Transactions acquire an exclusive lock *before* computing the current balance, serializing write operations and eliminating the race window completely.

---

## 🔬 Live Chaos Engineering Lab (`concurrency_attack.go`)

To mathematically prove the concurrency defense, `concurrency_attack.go` simulates a multi-worker attack:
* **Starting Balance:** `$75.00` (7,500 minor units).
* **Attack Scenario:** 10 parallel Goroutines fire simultaneously, each attempting to withdraw `$50.00` (5,000 minor units).
* **Total Attempted Withdrawal:** `$500.00`.

### Benchmark Results

| Metric | Stage A (Naive Check-Then-Act) | Stage B (Armored Serialized Defense) |
| :--- | :--- | :--- |
| **Concurrency Protection** | None (Unprotected read) | Pessimistic Transaction Lock |
| **Successful Withdrawals** | **10 / 10** (Double-Spend Catastrophe) | **1 / 10** (Exact Invariant Maintained) |
| **Failed / Rejected Attempts**| 0 | **9 / 10** (Correctly blocked: Insufficient Funds) |
| **Total Money Drained** | **$500.00** on a $75.00 balance | **$50.00** |
| **Final Account Balance** | **-$425.00** (Unauthorized overdraft) | **$25.00** (100% Solvency Preserved) |

---

## 🚀 Running the Code

### Prerequisites
* Go 1.21+ installed on your machine.

### 1. Run the Core Ledger Engine
Demonstrates account initialization, inbound deposit, compensating audit reversal, and rejection of unbalanced transactions:
```bash
go run main.go
```

### 2. Run the Chaos Concurrency Attack
Fires 10 parallel Goroutines against both naive and armored implementations to reproduce the race condition and prove the lock defense:
```bash
go run concurrency_attack.go
```

---

## 📂 Project Structure

```
├── main.go                # Double-entry ledger core, schema, and balance computation
├── concurrency_attack.go  # Concurrency chaos lab (10 parallel goroutine benchmark)
├── go.mod                 # Go module definition (pure Go SQLite driver)
└── README.md              # Technical architecture and benchmark documentation
```

---

## 👤 Author
**Joseph Nwankwo**  
*AI-Native Full-Stack Engineer*  
* Portfolio: [joeyejike.vercel.app](https://joeyejike.vercel.app)
* GitHub: [@jhozie](https://github.com/jhozie)
* Email: josephcgs02@gmail.com
