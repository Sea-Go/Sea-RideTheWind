package model

import (
	"context"
	"fmt"
)

// CheckProductSearchSchema refuses to serve a configured product façade on an
// old database. Production normally has Postgres.Migrate=false, so operators
// apply the versioned SQL before enabling SearchSummary.
func (s *Store) CheckProductSearchSchema(ctx context.Context) error {
	rows, err := s.DB.Query(ctx, `SELECT authority_id,tenant_id,subject_id,session_id,operation_key,
 request_hash,request_json,snapshot,search_id,answer_id,status,attempt,lease_token,lease_until,
 last_error_code FROM knowledge_product_search_operations LIMIT 0`)
	if err != nil {
		return fmt.Errorf("product search operation schema unavailable: %w", err)
	}
	rows.Close()
	return rows.Err()
}
