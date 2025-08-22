package auth

import (
	"database/sql"
	"testing"
	"time"

	"github.com/84adam/Arkfile/crypto"
	"github.com/pquerna/otp/totp"
)

func setupTOTPTestDB(t *testing.T) *sql.DB {
	// Use in-memory SQLite for testing
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	// Create the required tables for TOTP testing
	schema := `
		CREATE TABLE user_totp (
			username TEXT PRIMARY KEY,
			secret_encrypted BLOB NOT NULL,
			backup_codes_encrypted BLOB NOT NULL,
			enabled BOOLEAN DEFAULT FALSE,
			setup_completed BOOLEAN DEFAULT FALSE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_used DATETIME
		);

		CREATE TABLE totp_usage_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT NOT NULL,
			code_hash TEXT NOT NULL,
			window_start INTEGER NOT NULL,
			used_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE totp_backup_usage (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT NOT NULL,
			code_hash TEXT NOT NULL,
			used_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`

	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("Failed to create test schema: %v", err)
	}

	return db
}

func TestServerSideTOTPKeyManagement(t *testing.T) {
	// Initialize TOTP master key for testing
	if err := crypto.InitializeTOTPMasterKey(); err != nil {
		t.Fatalf("Failed to initialize TOTP master key: %v", err)
	}

	username := "testuser"

	// Test key derivation consistency
	key1, err := crypto.DeriveTOTPUserKey(username)
	if err != nil {
		t.Fatalf("Failed to derive TOTP key 1: %v", err)
	}
	defer crypto.SecureZeroTOTPKey(key1)

	key2, err := crypto.DeriveTOTPUserKey(username)
	if err != nil {
		t.Fatalf("Failed to derive TOTP key 2: %v", err)
	}
	defer crypto.SecureZeroTOTPKey(key2)

	// Keys should be identical for the same user
	if len(key1) != len(key2) {
		t.Fatal("TOTP keys have different lengths")
	}

	for i := range key1 {
		if key1[i] != key2[i] {
			t.Fatal("TOTP keys are not identical")
		}
	}

	// Test that different users get different keys
	key3, err := crypto.DeriveTOTPUserKey("different_user")
	if err != nil {
		t.Fatalf("Failed to derive TOTP key 3: %v", err)
	}
	defer crypto.SecureZeroTOTPKey(key3)

	// Keys should be different for different users
	identical := true
	for i := range key1 {
		if key1[i] != key3[i] {
			identical = false
			break
		}
	}
	if identical {
		t.Fatal("TOTP keys for different users are identical")
	}
}

func TestTOTPSetup(t *testing.T) {
	// Initialize TOTP master key for testing
	if err := crypto.InitializeTOTPMasterKey(); err != nil {
		t.Fatalf("Failed to initialize TOTP master key: %v", err)
	}

	db := setupTOTPTestDB(t)
	defer db.Close()

	username := "testuser"

	// Test TOTP setup generation
	setup, err := GenerateTOTPSetup(username)
	if err != nil {
		t.Fatalf("Failed to generate TOTP setup: %v", err)
	}

	// Validate setup structure
	if setup.Secret == "" {
		t.Fatal("TOTP secret is empty")
	}
	if setup.QRCodeURL == "" {
		t.Fatal("TOTP QR code URL is empty")
	}
	if len(setup.BackupCodes) != BackupCodeCount {
		t.Fatalf("Expected %d backup codes, got %d", BackupCodeCount, len(setup.BackupCodes))
	}
	if setup.ManualEntry == "" {
		t.Fatal("TOTP manual entry is empty")
	}

	// Store the TOTP setup
	if err := StoreTOTPSetup(db, username, setup); err != nil {
		t.Fatalf("Failed to store TOTP setup: %v", err)
	}

	// Verify setup was stored correctly
	totpData, err := getTOTPData(db, username)
	if err != nil {
		t.Fatalf("Failed to retrieve TOTP data: %v", err)
	}

	if totpData.Enabled {
		t.Fatal("TOTP should not be enabled before completion")
	}
	if totpData.SetupCompleted {
		t.Fatal("TOTP setup should not be completed yet")
	}

	// Test that we can decrypt the stored secret
	decryptedSecret, err := decryptTOTPSecret(totpData.SecretEncrypted, username)
	if err != nil {
		t.Fatalf("Failed to decrypt TOTP secret: %v", err)
	}

	if decryptedSecret != setup.Secret {
		t.Fatal("Decrypted TOTP secret does not match original")
	}
}

func TestTOTPCompletion(t *testing.T) {
	// Initialize TOTP master key for testing
	if err := crypto.InitializeTOTPMasterKey(); err != nil {
		t.Fatalf("Failed to initialize TOTP master key: %v", err)
	}

	db := setupTOTPTestDB(t)
	defer db.Close()

	username := "testuser"

	// Generate and store TOTP setup
	setup, err := GenerateTOTPSetup(username)
	if err != nil {
		t.Fatalf("Failed to generate TOTP setup: %v", err)
	}

	if err := StoreTOTPSetup(db, username, setup); err != nil {
		t.Fatalf("Failed to store TOTP setup: %v", err)
	}

	// Generate a valid TOTP code
	currentCode, err := totp.GenerateCode(setup.Secret, time.Now().UTC())
	if err != nil {
		t.Fatalf("Failed to generate TOTP code: %v", err)
	}

	// Complete TOTP setup
	if err := CompleteTOTPSetup(db, username, currentCode); err != nil {
		t.Fatalf("Failed to complete TOTP setup: %v", err)
	}

	// Verify TOTP is now enabled
	enabled, err := IsUserTOTPEnabled(db, username)
	if err != nil {
		t.Fatalf("Failed to check TOTP status: %v", err)
	}
	if !enabled {
		t.Fatal("TOTP should be enabled after completion")
	}

	// Test invalid code during setup completion
	setup2, err := GenerateTOTPSetup("testuser2")
	if err != nil {
		t.Fatalf("Failed to generate second TOTP setup: %v", err)
	}

	if err := StoreTOTPSetup(db, "testuser2", setup2); err != nil {
		t.Fatalf("Failed to store second TOTP setup: %v", err)
	}

	// Try to complete with invalid code
	if err := CompleteTOTPSetup(db, "testuser2", "000000"); err == nil {
		t.Fatal("TOTP setup completion should fail with invalid code")
	}
}

func TestTOTPValidation(t *testing.T) {
	// Initialize TOTP master key for testing
	if err := crypto.InitializeTOTPMasterKey(); err != nil {
		t.Fatalf("Failed to initialize TOTP master key: %v", err)
	}

	db := setupTOTPTestDB(t)
	defer db.Close()

	username := "testuser"

	// Set up and complete TOTP
	setup, err := GenerateTOTPSetup(username)
	if err != nil {
		t.Fatalf("Failed to generate TOTP setup: %v", err)
	}

	if err := StoreTOTPSetup(db, username, setup); err != nil {
		t.Fatalf("Failed to store TOTP setup: %v", err)
	}

	currentCode, err := totp.GenerateCode(setup.Secret, time.Now().UTC())
	if err != nil {
		t.Fatalf("Failed to generate TOTP code: %v", err)
	}

	if err := CompleteTOTPSetup(db, username, currentCode); err != nil {
		t.Fatalf("Failed to complete TOTP setup: %v", err)
	}

	// Test valid TOTP code validation
	testTime := time.Now().UTC()
	validCode, err := totp.GenerateCode(setup.Secret, testTime)
	if err != nil {
		t.Fatalf("Failed to generate valid TOTP code: %v", err)
	}

	if err := ValidateTOTPCode(db, username, validCode); err != nil {
		t.Fatalf("Valid TOTP code should be accepted: %v", err)
	}

	// Test invalid TOTP code
	if err := ValidateTOTPCode(db, username, "000000"); err == nil {
		t.Fatal("Invalid TOTP code should be rejected")
	}

	// Test replay attack prevention
	if err := ValidateTOTPCode(db, username, validCode); err == nil {
		t.Fatal("TOTP code replay should be prevented")
	}
}

func TestBackupCodeValidation(t *testing.T) {
	// Initialize TOTP master key for testing
	if err := crypto.InitializeTOTPMasterKey(); err != nil {
		t.Fatalf("Failed to initialize TOTP master key: %v", err)
	}

	db := setupTOTPTestDB(t)
	defer db.Close()

	username := "testuser"

	// Set up and complete TOTP
	setup, err := GenerateTOTPSetup(username)
	if err != nil {
		t.Fatalf("Failed to generate TOTP setup: %v", err)
	}

	if err := StoreTOTPSetup(db, username, setup); err != nil {
		t.Fatalf("Failed to store TOTP setup: %v", err)
	}

	currentCode, err := totp.GenerateCode(setup.Secret, time.Now().UTC())
	if err != nil {
		t.Fatalf("Failed to generate TOTP code: %v", err)
	}

	if err := CompleteTOTPSetup(db, username, currentCode); err != nil {
		t.Fatalf("Failed to complete TOTP setup: %v", err)
	}

	// Test valid backup code
	firstBackupCode := setup.BackupCodes[0]
	if err := ValidateBackupCode(db, username, firstBackupCode); err != nil {
		t.Fatalf("Valid backup code should be accepted: %v", err)
	}

	// Test backup code replay prevention
	if err := ValidateBackupCode(db, username, firstBackupCode); err == nil {
		t.Fatal("Backup code replay should be prevented")
	}

	// Test invalid backup code
	if err := ValidateBackupCode(db, username, "INVALIDCODE"); err == nil {
		t.Fatal("Invalid backup code should be rejected")
	}
}

func TestTOTPCleanup(t *testing.T) {
	// Initialize TOTP master key for testing
	if err := crypto.InitializeTOTPMasterKey(); err != nil {
		t.Fatalf("Failed to initialize TOTP master key: %v", err)
	}

	db := setupTOTPTestDB(t)
	defer db.Close()

	// Test cleanup function doesn't error
	if err := CleanupTOTPLogs(db); err != nil {
		t.Fatalf("TOTP cleanup failed: %v", err)
	}
}

func TestTOTPDisable(t *testing.T) {
	// Initialize TOTP master key for testing
	if err := crypto.InitializeTOTPMasterKey(); err != nil {
		t.Fatalf("Failed to initialize TOTP master key: %v", err)
	}

	db := setupTOTPTestDB(t)
	defer db.Close()

	username := "testuser"

	// Set up and complete TOTP
	setup, err := GenerateTOTPSetup(username)
	if err != nil {
		t.Fatalf("Failed to generate TOTP setup: %v", err)
	}

	if err := StoreTOTPSetup(db, username, setup); err != nil {
		t.Fatalf("Failed to store TOTP setup: %v", err)
	}

	currentCode, err := totp.GenerateCode(setup.Secret, time.Now().UTC())
	if err != nil {
		t.Fatalf("Failed to generate TOTP code: %v", err)
	}

	if err := CompleteTOTPSetup(db, username, currentCode); err != nil {
		t.Fatalf("Failed to complete TOTP setup: %v", err)
	}

	// Verify TOTP is enabled
	enabled, err := IsUserTOTPEnabled(db, username)
	if err != nil {
		t.Fatalf("Failed to check TOTP status: %v", err)
	}
	if !enabled {
		t.Fatal("TOTP should be enabled")
	}

	// Generate a current TOTP code for disabling
	disableCode, err := totp.GenerateCode(setup.Secret, time.Now().UTC())
	if err != nil {
		t.Fatalf("Failed to generate disable TOTP code: %v", err)
	}

	// Disable TOTP
	if err := DisableTOTP(db, username, disableCode); err != nil {
		t.Fatalf("Failed to disable TOTP: %v", err)
	}

	// Verify TOTP is disabled
	enabled, err = IsUserTOTPEnabled(db, username)
	if err != nil {
		t.Fatalf("Failed to check TOTP status after disable: %v", err)
	}
	if enabled {
		t.Fatal("TOTP should be disabled")
	}

	// Test invalid code for disable
	setup2, err := GenerateTOTPSetup("testuser2")
	if err != nil {
		t.Fatalf("Failed to generate second TOTP setup: %v", err)
	}

	if err := StoreTOTPSetup(db, "testuser2", setup2); err != nil {
		t.Fatalf("Failed to store second TOTP setup: %v", err)
	}

	currentCode2, err := totp.GenerateCode(setup2.Secret, time.Now().UTC())
	if err != nil {
		t.Fatalf("Failed to generate TOTP code: %v", err)
	}

	if err := CompleteTOTPSetup(db, "testuser2", currentCode2); err != nil {
		t.Fatalf("Failed to complete TOTP setup: %v", err)
	}

	// Try to disable with invalid code
	if err := DisableTOTP(db, "testuser2", "000000"); err == nil {
		t.Fatal("TOTP disable should fail with invalid code")
	}
}
