package main

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "modernc.org/sqlite"
)

// ==============================================================================
// LLEMON CORE FINANCIAL ENGINE: DOUBLE-ENTRY LEDGER (PHASE 1)
// ==============================================================================
// Rule 1: All money amounts are stored in MINOR UNITS (integers: kobo, pence, cents).
// Rule 2: There is NO mutable "balance" column on accounts. Balance is computed.
// Rule 3: Debits must equal Credits for every transaction: SUM(Entries) = 0.
// Rule 4: Audit Immutability: No UPDATE or DELETE allowed on entries.
// ==============================================================================

type Account struct {
	ID        string
	OwnerName string
	Currency  string
	Type      string // USER_WALLET, PLATFORM_TREASURY, FX_LIQUIDITY_POOL
}

func main() {
	fmt.Println("==================================================")
	fmt.Println("🚀 Initializing LLemon Core Ledger (Go + SQLite)")
	fmt.Println("==================================================")

	// 1. Connect to SQLite in WAL (Write-Ahead Logging) mode for ACID guarantees
	db, err := sql.Open("sqlite", "llemon_ledger.db?_journal_mode=WAL")
	if err != nil {
		log.Fatalf("❌ Failed to open database: %v", err)
	}
	defer db.Close()

	// 2. Initialize the immutable schema
	initSchema(db)

	// 3. Create sample accounts: User Wallet and Platform Treasury
	createAccount(db, "acc_joseph_ngn", "Joseph Nwankwo", "NGN", "USER_WALLET")
	createAccount(db, "acc_llemon_ngn_treasury", "LLemon Treasury", "NGN", "PLATFORM_TREASURY")
	createAccount(db, "acc_joseph_gbp", "Joseph Nwankwo", "GBP", "USER_WALLET")

	// 4. Initial State: Check balance
	printBalance(db, "acc_joseph_ngn")

	// 5. Simulate Inbound Deposit: User deposits ₦100,000 (10,000,000 kobo)
	// In double-entry:
	// - LLemon Treasury receives physical bank funds (DEBIT +10,000,000)
	// - User Wallet is credited with digital balance (CREDIT +10,000,000)
	fmt.Println("\n💳 [STEP 1] User deposits ₦100,000 into Nigerian Wallet...")
	err = recordTransaction(db, "tx_deposit_001", "Inbound Bank Transfer via NIBSS", []Entry{
		{AccountID: "acc_joseph_ngn", AmountMinor: 10000000, Direction: "CREDIT"},
		{AccountID: "acc_llemon_ngn_treasury", AmountMinor: 10000000, Direction: "DEBIT"},
	})
	if err != nil {
		log.Fatalf("❌ Transaction failed: %v", err)
	}
	printBalance(db, "acc_joseph_ngn")

	// 6. Demonstrate Audit Immutability:
	// A refund or mistake happens (e.g., ₦25,000 reversal).
	// We DO NOT update or delete the previous transaction!
	// We append a new COMPENSATING TRANSACTION.
	fmt.Println("\n🔄 [STEP 2] Demonstrating Audit Immutability: Reversing ₦25,000...")
	err = recordTransaction(db, "tx_reversal_002", "Partial Refund / Chargeback Reversal", []Entry{
		{AccountID: "acc_joseph_ngn", AmountMinor: 2500000, Direction: "DEBIT"},
		{AccountID: "acc_llemon_ngn_treasury", AmountMinor: 2500000, Direction: "CREDIT"},
	})
	if err != nil {
		log.Fatalf("❌ Reversal failed: %v", err)
	}
	printBalance(db, "acc_joseph_ngn")

	// 7. Audit Trail Verification: Inspect every immutable entry in the history
	printAuditHistory(db, "acc_joseph_ngn")

	// 8. Test Invalid Double-Entry Rejection (Intentional failure test):
	// What happens if someone tries to create money out of thin air? (Debits != Credits)
	fmt.Println("\n🛡️ [STEP 3] Testing Double-Entry Invariant (Creating unbalanced transaction)...")
	err = recordTransaction(db, "tx_bad_003", "Hacker Attempt: Unbalanced Money Creation", []Entry{
		{AccountID: "acc_joseph_ngn", AmountMinor: 99999999, Direction: "CREDIT"},
		// Missing corresponding debit!
	})
	if err != nil {
		fmt.Printf("✅ As expected, ledger rejected unbalanced transaction: %v\n", err)
	} else {
		log.Fatalf("❌ CRITICAL BUG: System allowed unbalanced transaction!")
	}
}

// -----------------------------------------------------------------------------
// LEDGER CORE FUNCTIONS
// -----------------------------------------------------------------------------

type Entry struct {
	AccountID   string
	AmountMinor int64  // Positive integer (kobo, pence, cents)
	Direction   string // "CREDIT" or "DEBIT"
}

func initSchema(db *sql.DB) {
	schema := `
	CREATE TABLE IF NOT EXISTS accounts (
		id TEXT PRIMARY KEY,
		owner_name TEXT NOT NULL,
		currency TEXT NOT NULL,
		account_type TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS transactions (
		id TEXT PRIMARY KEY,
		description TEXT NOT NULL,
		status TEXT NOT NULL, -- PENDING, POSTED, REVERSED
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS entries (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		transaction_id TEXT NOT NULL,
		account_id TEXT NOT NULL,
		amount_minor INTEGER NOT NULL CHECK (amount_minor > 0),
		direction TEXT NOT NULL CHECK (direction IN ('DEBIT', 'CREDIT')),
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (transaction_id) REFERENCES transactions(id),
		FOREIGN KEY (account_id) REFERENCES accounts(id)
	);

	CREATE INDEX IF NOT EXISTS idx_entries_account ON entries(account_id);
	`
	_, err := db.Exec(schema)
	if err != nil {
		log.Fatalf("❌ Schema init error: %v", err)
	}
}

func createAccount(db *sql.DB, id, owner, currency, accType string) {
	query := `INSERT OR IGNORE INTO accounts (id, owner_name, currency, account_type) VALUES (?, ?, ?, ?)`
	_, err := db.Exec(query, id, owner, currency, accType)
	if err != nil {
		log.Fatalf("❌ Failed to create account %s: %v", id, err)
	}
}

// recordTransaction writes an immutable double-entry transaction inside an ACID transaction block.
func recordTransaction(db *sql.DB, txID, description string, entries []Entry) error {
	// Step 1: Pre-validation of Double-Entry rule: Sum of Debits MUST equal Sum of Credits
	var totalDebits, totalCredits int64
	for _, e := range entries {
		if e.AmountMinor <= 0 {
			return fmt.Errorf("entry amount must be greater than zero, got %d", e.AmountMinor)
		}
		if e.Direction == "DEBIT" {
			totalDebits += e.AmountMinor
		} else if e.Direction == "CREDIT" {
			totalCredits += e.AmountMinor
		} else {
			return fmt.Errorf("invalid direction: %s", e.Direction)
		}
	}

	if totalDebits != totalCredits {
		return fmt.Errorf("double-entry violation: debits (%d) != credits (%d)", totalDebits, totalCredits)
	}

	// Step 2: Begin ACID Database Transaction
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin db transaction: %w", err)
	}
	defer tx.Rollback()

	// Step 3: Insert parent transaction record
	_, err = tx.Exec(`INSERT INTO transactions (id, description, status) VALUES (?, ?, ?)`,
		txID, description, "POSTED")
	if err != nil {
		return fmt.Errorf("failed to insert transaction record: %w", err)
	}

	// Step 4: Insert immutable individual entries
	for _, e := range entries {
		_, err = tx.Exec(
			`INSERT INTO entries (transaction_id, account_id, amount_minor, direction) VALUES (?, ?, ?, ?)`,
			txID, e.AccountID, e.AmountMinor, e.Direction,
		)
		if err != nil {
			return fmt.Errorf("failed to insert entry for %s: %w", e.AccountID, err)
		}
	}

	// Step 5: Commit transaction atomically
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit failed: %w", err)
	}

	return nil
}

// getBalance computes current balance dynamically from the immutable entries table.
func getBalance(db *sql.DB, accountID string) (int64, string, error) {
	var currency string
	err := db.QueryRow(`SELECT currency FROM accounts WHERE id = ?`, accountID).Scan(&currency)
	if err != nil {
		return 0, "", err
	}

	query := `
	SELECT 
		COALESCE(SUM(CASE WHEN direction = 'CREDIT' THEN amount_minor ELSE -amount_minor END), 0) as balance
	FROM entries
	WHERE account_id = ?
	`
	var balanceMinor int64
	err = db.QueryRow(query, accountID).Scan(&balanceMinor)
	if err != nil {
		return 0, "", err
	}

	return balanceMinor, currency, nil
}

func printBalance(db *sql.DB, accountID string) {
	balanceMinor, currency, err := getBalance(db, accountID)
	if err != nil {
		log.Fatalf("❌ Balance check failed: %v", err)
	}
	major := float64(balanceMinor) / 100.0
	fmt.Printf("💰 Current Balance for [%s]: %.2f %s (%d minor units)\n", accountID, major, currency, balanceMinor)
}

func printAuditHistory(db *sql.DB, accountID string) {
	fmt.Printf("\n📜 --- AUDIT TRAIL FOR [%s] ---\n", accountID)
	rows, err := db.Query(`
		SELECT e.id, e.transaction_id, t.description, e.direction, e.amount_minor, e.created_at
		FROM entries e
		JOIN transactions t ON e.transaction_id = t.id
		WHERE e.account_id = ?
		ORDER BY e.id ASC
	`, accountID)
	if err != nil {
		log.Fatalf("❌ Failed to query audit trail: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id          int
			txID, desc  string
			direction   string
			amountMinor int64
			createdAt   time.Time
		)
		if err := rows.Scan(&id, &txID, &desc, &direction, &amountMinor, &createdAt); err != nil {
			log.Fatalf("❌ Row scan error: %v", err)
		}
		sign := "+"
		if direction == "DEBIT" {
			sign = "-"
		}
		fmt.Printf("  Entry #%d | %s | %s%.2f | %s | TxID: %s\n",
			id, createdAt.Format("15:04:05"), sign, float64(amountMinor)/100.0, desc, txID)
	}
	fmt.Println("---------------------------------------------------------")
}
