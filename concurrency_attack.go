package main

import (
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// ==============================================================================
// FINANCIAL ENGINE CHAOS LAB: CONCURRENCY & RACE CONDITION DEFENSE
// ==============================================================================
// Scenario: Joseph has $75.00 (7,500 cents / minor units) in his wallet.
// 10 concurrent goroutines each attempt to withdraw $50.00 (5,000 cents).
// Total attempted withdrawal: $500.00.
// Correct invariant result: Exactly 1 succeeds, 9 fail with "Insufficient Funds".
// Vulnerable result: Multiple succeed, allowing overdraft / double-spending.
// ==============================================================================

const (
	InitialBalance = 7500 // $75.00 in minor units (cents)
	WithdrawAmount = 5000 // $50.00 in minor units (cents)
	TotalAttackers = 10   // 10 concurrent workers
)

func main() {
	fmt.Println("==================================================================")
	fmt.Println("⚔️  CHAOS LAB: THE 10-GOROUTINE CONCURRENCY & RACE ATTACK")
	fmt.Println("==================================================================")

	db, err := sql.Open("sqlite", "file:chaos_lab.db?_journal_mode=WAL&cache=shared&_busy_timeout=5000")
	if err != nil {
		log.Fatalf("❌ Failed to open database: %v", err)
	}
	defer db.Close()

	// =========================================================================
	// 🚨 STAGE A: THE VULNERABLE WITHDRAWAL (No Locks, Naive Check-Then-Act)
	// =========================================================================
	setupChaosDatabase(db)
	seedInitialBalance(db, InitialBalance)
	printWalletBalance(db, "STAGE A - STARTING BALANCE")

	fmt.Println("\n💥 [STAGE A] Launching 10 parallel attackers against UNPROTECTED endpoint...")
	runAttack(db, naiveVulnerableWithdraw)
	printWalletBalance(db, "STAGE A - FINAL BALANCE")

	// =========================================================================
	// 🛡️ STAGE B: THE ARMORED DEFENSE (Pessimistic Locks & Atomic Transaction)
	// =========================================================================
	fmt.Println("\n==================================================================")
	fmt.Println("🛡️  STAGE B: ACTIVATING THE SERIALIZED ATOMIC LOCK DEFENSE")
	fmt.Println("==================================================================")

	setupChaosDatabase(db)
	seedInitialBalance(db, InitialBalance)
	printWalletBalance(db, "STAGE B - STARTING BALANCE")

	fmt.Println("\n🔒 [STAGE B] Launching 10 parallel attackers against ARMORED endpoint...")
	runAttack(db, armoredProtectedWithdraw)
	printWalletBalance(db, "STAGE B - FINAL BALANCE")
}

func runAttack(db *sql.DB, withdrawFunc func(*sql.DB, int, int64) error) {
	var wg sync.WaitGroup
	var successCount, failCount int
	var countLock sync.Mutex

	startTime := time.Now()

	for i := 1; i <= TotalAttackers; i++ {
		wg.Add(1)

		go func(workerID int) {
			defer wg.Done()

			err := withdrawFunc(db, workerID, WithdrawAmount)

			countLock.Lock()
			if err == nil {
				successCount++
			} else {
				failCount++
			}
			countLock.Unlock()
		}(i)
	}

	wg.Wait()

	fmt.Printf("\n⏱️  Attack finished in %v\n", time.Since(startTime))
	fmt.Printf("   Successful Transactions: %d  (Target: 1)\n", successCount)
	fmt.Printf("   Failed Transactions:     %d  (Target: 9)\n", failCount)

	if successCount > 1 {
		fmt.Printf("🚨 VULNERABILITY EXPLOITED: System allowed %d double-spends! Balance was breached!\n", successCount)
	} else if successCount == 1 {
		fmt.Println("✅ DEFENSE HELD FIRM: Exactly 1 transaction succeeded; all 9 duplicate attacks were crushed!")
	} else {
		fmt.Println("⚠️ Unexpected: 0 withdrawals succeeded.")
	}
}

// -----------------------------------------------------------------------------
// STAGE A: THE VULNERABLE CODE (Naive Check-Then-Act)
// -----------------------------------------------------------------------------
func naiveVulnerableWithdraw(db *sql.DB, workerID int, amountMinor int64) error {
	// Step 1: Read balance outside of any lock
	balance, err := computeBalance(db, "acc_wallet")
	if err != nil {
		return err
	}

	// Step 2: 5ms processing window (the race condition trap)
	time.Sleep(5 * time.Millisecond)

	// Step 3: Check
	if balance < amountMinor {
		fmt.Printf("   ❌ [Worker #%02d] Rejected: Insufficient balance (saw $%.2f)\n",
			workerID, float64(balance)/100.0)
		return fmt.Errorf("insufficient funds")
	}

	// Step 4: Act (Deduct without lock)
	txID := fmt.Sprintf("tx_naive_w%d_%d", workerID, time.Now().UnixNano())
	_, err = db.Exec(`
		INSERT INTO entries (transaction_id, account_id, amount_minor, direction) 
		VALUES (?, 'acc_wallet', ?, 'DEBIT')
	`, txID, amountMinor)
	if err != nil {
		return err
	}

	fmt.Printf("   💸 [Worker #%02d] SUCCESS: Withdrew $%.2f (Saw balance $%.2f)\n",
		workerID, float64(amountMinor)/100.0, float64(balance)/100.0)
	return nil
}

// -----------------------------------------------------------------------------
// STAGE B: THE ARMORED DEFENSE (Pessimistic Lock & Atomic Transaction)
// -----------------------------------------------------------------------------
func armoredProtectedWithdraw(db *sql.DB, workerID int, amountMinor int64) error {
	// Step 1: Acquire exclusive write lock immediately
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}
	defer tx.Rollback()

	// Step 2: Read the balance INSIDE the protected transaction
	var currentBalance int64
	query := `
		SELECT COALESCE(SUM(CASE WHEN direction = 'CREDIT' THEN amount_minor ELSE -amount_minor END), 0)
		FROM entries WHERE account_id = 'acc_wallet'
	`
	err = tx.QueryRow(query).Scan(&currentBalance)
	if err != nil {
		return err
	}

	// Step 3: Processing latency INSIDE the lock
	time.Sleep(5 * time.Millisecond)

	// Step 4: Inspect balance
	if currentBalance < amountMinor {
		fmt.Printf("   🛡️ [Worker #%02d] DEFENSE BLOCKED: Insufficient balance (current balance is $%.2f)\n",
			workerID, float64(currentBalance)/100.0)
		return fmt.Errorf("insufficient funds")
	}

	// Step 5: Deduct funds
	txID := fmt.Sprintf("tx_armored_w%d_%d", workerID, time.Now().UnixNano())
	_, err = tx.Exec(`
		INSERT INTO entries (transaction_id, account_id, amount_minor, direction) 
		VALUES (?, 'acc_wallet', ?, 'DEBIT')
	`, txID, amountMinor)
	if err != nil {
		return err
	}

	// Step 6: Commit transaction and release lock for the next serialized worker
	if err := tx.Commit(); err != nil {
		return err
	}

	fmt.Printf("   ✅ [Worker #%02d] AUTHORIZED: Withdrew $%.2f (Remaining balance: $%.2f)\n",
		workerID, float64(amountMinor)/100.0, float64(currentBalance-amountMinor)/100.0)
	return nil
}

// -----------------------------------------------------------------------------
// HELPER SETUP FUNCTIONS
// -----------------------------------------------------------------------------

func setupChaosDatabase(db *sql.DB) {
	_, _ = db.Exec(`DROP TABLE IF EXISTS entries; DROP TABLE IF EXISTS accounts;`)
	schema := `
	CREATE TABLE accounts (
		id TEXT PRIMARY KEY,
		owner_name TEXT NOT NULL
	);
	CREATE TABLE entries (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		transaction_id TEXT NOT NULL,
		account_id TEXT NOT NULL,
		amount_minor INTEGER NOT NULL,
		direction TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`
	if _, err := db.Exec(schema); err != nil {
		log.Fatalf("❌ Schema setup failed: %v", err)
	}
	_, _ = db.Exec(`INSERT INTO accounts (id, owner_name) VALUES ('acc_wallet', 'Test Account')`)
}

func seedInitialBalance(db *sql.DB, amountMinor int64) {
	_, err := db.Exec(`
		INSERT INTO entries (transaction_id, account_id, amount_minor, direction) 
		VALUES ('seed_deposit_001', 'acc_wallet', ?, 'CREDIT')
	`, amountMinor)
	if err != nil {
		log.Fatalf("❌ Seeding failed: %v", err)
	}
}

func computeBalance(db *sql.DB, accountID string) (int64, error) {
	query := `
	SELECT COALESCE(SUM(CASE WHEN direction = 'CREDIT' THEN amount_minor ELSE -amount_minor END), 0)
	FROM entries WHERE account_id = ?
	`
	var balance int64
	err := db.QueryRow(query, accountID).Scan(&balance)
	return balance, err
}

func printWalletBalance(db *sql.DB, title string) {
	bal, _ := computeBalance(db, "acc_wallet")
	fmt.Printf("💰 %s: $%.2f (%d minor units)\n", title, float64(bal)/100.0, bal)
}
