package requests

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/sjfehlen/mediamesh/internal/catalog"
)

// CheckDuplicate checks if a catalog item already exists locally.
// Returns (isDuplicate, reason, error).
func CheckDuplicate(ctx context.Context, db *sql.DB, item *catalog.Item) (bool, string, error) {
	// Check 1: exact path match.
	var count int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM library_items WHERE peer_id IS NULL AND relative_path = ?`,
		item.RelativePath,
	).Scan(&count)
	if err != nil {
		return false, "", fmt.Errorf("check exact path: %w", err)
	}
	if count > 0 {
		return true, "exact_path_match", nil
	}

	// Check 2: title + file size match.
	if item.FileSize != nil {
		err = db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM library_items WHERE peer_id IS NULL AND title = ? AND file_size = ?`,
			item.Title, *item.FileSize,
		).Scan(&count)
		if err != nil {
			return false, "", fmt.Errorf("check size+title: %w", err)
		}
		if count > 0 {
			return true, "size_title_match", nil
		}
	}

	return false, "", nil
}
