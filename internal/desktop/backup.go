package desktop

// BackupRequest selects an export directory or an existing database to import.
// Import is false for export. Hosts enqueue requests; the service worker owns
// serialization, database access, validation and configuration changes.
type BackupRequest struct {
	Import bool
	Path   string
}
