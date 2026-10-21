package migrate

import "errors"

var (
	// ErrPendingMigration means that integrity verification found an unapplied
	// migration after verifying all recorded migrations. It does not apply it.
	ErrPendingMigration = errors.New("migrate: pending migration")
	// ErrMigrationDrift means a recorded migration no longer matches its source,
	// or a row has lost its checksum despite an integrity-enabled ledger. Removal
	// of an entire source pair, changed names, and changed up-script checksums are
	// drift. An incomplete or inconsistently named pair is invalid source instead.
	ErrMigrationDrift = errors.New("migrate: migration drift")
	// ErrInvalidMigrationSource means discovered migration source has an incomplete
	// pair, an empty script, or inconsistent names for one version. Source is
	// validated before ledger inspection, so this does not establish whether the
	// affected version was applied. Filesystem I/O errors are not this category.
	ErrInvalidMigrationSource = errors.New("migrate: invalid migration source")
	// ErrLedgerUpgradeRequired means the ledger is uninitialized or uses legacy
	// checksum bookkeeping. Verify never creates or upgrades the ledger.
	ErrLedgerUpgradeRequired = errors.New("migrate: ledger upgrade required")
)
