package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// FavoriteLegacyPreflightReport is a read-only, repeatable-read snapshot of
// business favorites without a frozen assertion. It cannot decide whether an
// unmarked row is legitimate old data; the migration owner must approve its
// source snapshot or explicitly block the release.
type FavoriteLegacyPreflightReport struct {
	Contract       string `json:"contract"`
	MissingAssert  int64  `json:"missing_assert"`
	ApprovedLegacy int64  `json:"approved_legacy"`
	Blocked        int64  `json:"blocked"`
	OrphanRetracts int64  `json:"orphan_retracts"`
}

func (r FavoriteLegacyPreflightReport) Clear() bool {
	return r.Contract == "rtw.favorite.legacy-preflight.v1" &&
		r.MissingAssert == r.ApprovedLegacy && r.Blocked == 0 && r.OrphanRetracts == 0
}

// FavoriteLegacyPreflight never mutates business, markers or Outbox. A clear
// local report is only one release gate: real source writers must be frozen
// while operators enumerate, approve and re-run the production snapshot.
func (m *FavoriteModel) FavoriteLegacyPreflight(ctx context.Context) (FavoriteLegacyPreflightReport, error) {
	report := FavoriteLegacyPreflightReport{Contract: "rtw.favorite.legacy-preflight.v1"}
	if m == nil || m.conn == nil {
		return report, errors.New("favorite preflight database unavailable")
	}
	tx := m.conn.WithContext(ctx).Begin(&sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if tx.Error != nil {
		return report, tx.Error
	}
	defer tx.Rollback()
	rows, err := tx.Raw(`SELECT i.favorite_id,i.user_id,i.folder_id,i.target_type,i.target_id,i.target_revision,
	 m.favorite_id,m.user_id,m.folder_id,m.target_type,m.target_id,m.target_revision,
	 m.source_snapshot_sha256,m.approval_ref
	 FROM favorite_item i
	 LEFT JOIN favorite_fact_outbox assertion
	  ON assertion.favorite_id=i.favorite_id AND assertion.aggregate_version=1
	 LEFT JOIN favorite_legacy_fact_marker m ON m.favorite_id=i.favorite_id
	 WHERE assertion.event_id IS NULL ORDER BY i.favorite_id`).Rows()
	if err != nil {
		return report, fmt.Errorf("read favorite legacy migration graph: %w", err)
	}
	for rows.Next() {
		var item FavoriteItem
		var markerID, markerUID, markerFolder sql.NullInt64
		var markerType, markerTarget, markerRevision, markerHash, markerApproval sql.NullString
		var revision sql.NullString
		if err := rows.Scan(&item.FavoriteId, &item.UserId, &item.FolderId,
			&item.TargetType, &item.TargetId, &revision, &markerID, &markerUID, &markerFolder,
			&markerType, &markerTarget, &markerRevision, &markerHash, &markerApproval); err != nil {
			rows.Close()
			return report, err
		}
		if revision.Valid {
			item.TargetRevision = &revision.String
		}
		report.MissingAssert++
		hash, err := favoriteLegacySnapshotHash(item)
		if err != nil || !markerID.Valid || !markerUID.Valid || !markerFolder.Valid ||
			!markerType.Valid || !markerTarget.Valid || !markerHash.Valid || !markerApproval.Valid ||
			markerID.Int64 != item.FavoriteId || markerUID.Int64 != item.UserId ||
			markerFolder.Int64 != item.FolderId || markerType.String != item.TargetType ||
			markerTarget.String != item.TargetId || markerHash.String != hash || markerApproval.String == "" ||
			markerRevision.Valid != revision.Valid ||
			(markerRevision.Valid && markerRevision.String != revision.String) {
			report.Blocked++
			continue
		}
		report.ApprovedLegacy++
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return report, err
	}
	if err := rows.Close(); err != nil {
		return report, err
	}
	if err := tx.Raw(`SELECT count(*) FROM favorite_fact_outbox retract
	 WHERE retract.aggregate_version=2 AND NOT EXISTS (
	  SELECT 1 FROM favorite_fact_outbox assertion
	  WHERE assertion.favorite_id=retract.favorite_id AND assertion.aggregate_version=1)`).Scan(&report.OrphanRetracts).Error; err != nil {
		return report, err
	}
	if err := tx.Commit().Error; err != nil {
		return report, err
	}
	return report, nil
}
