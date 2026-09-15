package model

import (
	"context"
	"strconv"
	"strings"
	"time"

	"sea-try-go/service/common/communityfact"
)

func deliveredCommentAuthority(row CommentDomainFactOutbox) (communityfact.AuthorityFact, commentFact, bool) {
	if row.DeliveryStatus != communityfact.DeliveryAccepted || row.DeliveredAt == nil ||
		row.TechnicalReceivedAt == nil || row.TechnicalReceiptID == "" || row.TechnicalOffset < 1 ||
		!communityfact.ValidHash(row.TechnicalInputHash) || row.DeliveryEnvelope == nil {
		return communityfact.AuthorityFact{}, commentFact{}, false
	}
	event, err := decodeCommentDelivery(*row.DeliveryEnvelope)
	if err != nil || !validCommentDelivery(row, event) {
		return communityfact.AuthorityFact{}, commentFact{}, false
	}
	var payload commentFact
	if err := communityfact.StrictDecode(strings.NewReader(string(event.Payload)), &payload); err != nil {
		return communityfact.AuthorityFact{}, commentFact{}, false
	}
	subject, ok := communityfact.ParseRTWSubject(payload.SubjectRef)
	if !ok || !validCommentRevision(payload.TargetRevision, payload.RevisionStatus) ||
		payload.SourceRef == "" || payload.EventTime.IsZero() || payload.AvailableAt.IsZero() {
		return communityfact.AuthorityFact{}, commentFact{}, false
	}
	hash, err := communityfact.CanonicalHash([]byte(*row.DeliveryEnvelope))
	if err != nil || hash != row.TechnicalInputHash {
		return communityfact.AuthorityFact{}, commentFact{}, false
	}
	receipt := communityfact.TechnicalReceipt{EventID: row.EventID, Producer: event.Producer,
		TechnicalStatus: "accepted", ReceiptID: row.TechnicalReceiptID, InputHash: row.TechnicalInputHash,
		Offset: row.TechnicalOffset, ReceivedAt: row.TechnicalReceivedAt.UTC().Format(time.RFC3339Nano)}
	return communityfact.AuthorityFact{Event: event, SubjectRef: subject,
		TechnicalReceipt: receipt, SourceEventHash: hash}, payload, true
}

func validCommentRevision(revision *string, status string) bool {
	if revision == nil {
		return status == "unknown"
	}
	return *revision != "" && status == "resolved"
}

// AuthoritativeFact returns source-owned subject, target and predecessor
// evidence only after the exact frozen EventSpec has a local DC receipt.
func (m *CommentModel) AuthoritativeFact(ctx context.Context, producer, eventID string) (communityfact.AuthorityFact, error) {
	if m == nil || m.conn == nil || producer != commentFactProducer || eventID == "" || len(eventID) > 128 {
		return communityfact.AuthorityFact{}, communityfact.ErrAuthorityUnavailable
	}
	var row CommentDomainFactOutbox
	query := m.conn.WithContext(ctx).Where("event_id = ?", eventID).Limit(1).Find(&row)
	if query.Error != nil {
		return communityfact.AuthorityFact{}, query.Error
	}
	if query.RowsAffected != 1 {
		return communityfact.AuthorityFact{}, communityfact.ErrAuthorityUnavailable
	}
	authority, payload, ok := deliveredCommentAuthority(row)
	if !ok {
		return communityfact.AuthorityFact{}, communityfact.ErrAuthorityUnavailable
	}
	commentID, err := strconv.ParseInt(payload.CommentID, 10, 64)
	if err != nil || commentID <= 0 || strconv.FormatInt(commentID, 10) != payload.CommentID {
		return communityfact.AuthorityFact{}, communityfact.ErrAuthorityUnavailable
	}
	var comment CommentIndex
	lookup := m.conn.WithContext(ctx).Where("id = ?", commentID).Limit(1).Find(&comment)
	if lookup.Error != nil {
		return communityfact.AuthorityFact{}, lookup.Error
	}
	if lookup.RowsAffected != 1 || comment.TargetType != payload.TargetType || comment.TargetId != payload.TargetID ||
		(payload.EventType != "community.comment.interaction" &&
			strconv.FormatInt(comment.UserId, 10) != strings.TrimPrefix(payload.SubjectRef, "rtw.identity/platform/")) {
		return communityfact.AuthorityFact{}, communityfact.ErrAuthorityUnavailable
	}
	switch payload.EventType {
	case "community.comment.created":
		if payload.Operation != "create" || payload.SourceRef != "rtw.comment/"+payload.CommentID ||
			eventID != payload.SourceRef+"/created" {
			return communityfact.AuthorityFact{}, communityfact.ErrAuthorityUnavailable
		}
		var contentCount int64
		if err := m.conn.WithContext(ctx).Model(&CommentContent{}).Where("comment_id = ?", commentID).Count(&contentCount).Error; err != nil {
			return communityfact.AuthorityFact{}, err
		}
		if contentCount != 1 {
			return communityfact.AuthorityFact{}, communityfact.ErrAuthorityUnavailable
		}
		if comment.State == 2 {
			var retractCount int64
			if err := m.conn.WithContext(ctx).Model(&CommentDomainFactOutbox{}).
				Where("event_id = ?", payload.SourceRef+"/deleted").Count(&retractCount).Error; err != nil {
				return communityfact.AuthorityFact{}, err
			}
			if retractCount != 1 {
				return communityfact.AuthorityFact{}, communityfact.ErrAuthorityUnavailable
			}
		}
		return authority, nil
	case "community.comment.deleted":
		if payload.Operation != "retract" || payload.SourceRef != "rtw.comment/"+payload.CommentID ||
			eventID != payload.SourceRef+"/deleted" || comment.State != 2 {
			return communityfact.AuthorityFact{}, communityfact.ErrAuthorityUnavailable
		}
		predecessorID := payload.SourceRef + "/created"
		var predecessor CommentDomainFactOutbox
		query := m.conn.WithContext(ctx).Where("event_id = ?", predecessorID).Limit(1).Find(&predecessor)
		if query.Error != nil {
			return communityfact.AuthorityFact{}, query.Error
		}
		prior, priorPayload, valid := deliveredCommentAuthority(predecessor)
		if query.RowsAffected != 1 || !valid || prior.TechnicalReceipt.Offset >= authority.TechnicalReceipt.Offset ||
			prior.SubjectRef != authority.SubjectRef || priorPayload.TargetType != payload.TargetType ||
			priorPayload.TargetID != payload.TargetID || priorPayload.SourceRef != payload.SourceRef ||
			!sameStringPointer(priorPayload.TargetRevision, payload.TargetRevision) {
			return communityfact.AuthorityFact{}, communityfact.ErrAuthorityUnavailable
		}
		authority.PredecessorEventID = predecessorID
		return authority, nil
	case "community.comment.interaction":
		return m.authoritativeCommentInteraction(ctx, authority, payload, row.FactVersion)
	default:
		return communityfact.AuthorityFact{}, communityfact.ErrAuthorityUnavailable
	}
}

func (m *CommentModel) authoritativeCommentInteraction(ctx context.Context, authority communityfact.AuthorityFact,
	payload commentFact, requestedVersion *int64) (communityfact.AuthorityFact, error) {
	if requestedVersion == nil || payload.OldState == nil || payload.NewState == nil ||
		payload.SourceRef != payload.EventID || !strings.HasPrefix(payload.EventID, "rtw.comment.interaction/") {
		return communityfact.AuthorityFact{}, communityfact.ErrAuthorityUnavailable
	}
	var rows []CommentDomainFactOutbox
	if err := m.conn.WithContext(ctx).
		Where("event_type = ? AND payload->>'subject_ref' = ? AND payload->>'comment_id' = ?",
			"community.comment.interaction", payload.SubjectRef, payload.CommentID).
		Order("fact_version,event_id").Find(&rows).Error; err != nil {
		return communityfact.AuthorityFact{}, err
	}
	state := int32(0)
	previous := ""
	found := false
	for _, candidate := range rows {
		if candidate.DeliveryEnvelope == nil || candidate.FactVersion == nil {
			return communityfact.AuthorityFact{}, communityfact.ErrAuthorityUnavailable
		}
		event, err := decodeCommentDelivery(*candidate.DeliveryEnvelope)
		if err != nil || !validCommentDelivery(candidate, event) {
			return communityfact.AuthorityFact{}, communityfact.ErrAuthorityUnavailable
		}
		var current commentFact
		if err := communityfact.StrictDecode(strings.NewReader(string(event.Payload)), &current); err != nil ||
			current.OldState == nil || current.NewState == nil || *current.OldState != state ||
			current.SubjectRef != payload.SubjectRef || current.CommentID != payload.CommentID ||
			current.TargetType != payload.TargetType || current.TargetID != payload.TargetID ||
			!sameStringPointer(current.TargetRevision, payload.TargetRevision) ||
			!validReactionTransition(current.Operation, *current.OldState, *current.NewState) {
			return communityfact.AuthorityFact{}, communityfact.ErrAuthorityUnavailable
		}
		if candidate.EventID == payload.EventID {
			found = true
			if *current.OldState != 0 {
				authority.PredecessorEventID = previous
			}
		}
		state = *current.NewState
		previous = candidate.EventID
	}
	if !found || (*payload.OldState != 0 && authority.PredecessorEventID == "") {
		return communityfact.AuthorityFact{}, communityfact.ErrAuthorityUnavailable
	}
	var record CommentLike
	lookup := m.conn.WithContext(ctx).Where("user_id = ? AND comment_id = ?", subjectID(authority.SubjectRef), payload.CommentID).
		Limit(1).Find(&record)
	if lookup.Error != nil {
		return communityfact.AuthorityFact{}, lookup.Error
	}
	if lookup.RowsAffected != 1 || record.State != state || record.TargetType != payload.TargetType || record.TargetId != payload.TargetID {
		return communityfact.AuthorityFact{}, communityfact.ErrAuthorityUnavailable
	}
	return authority, nil
}

func validReactionTransition(operation string, oldState, newState int32) bool {
	switch operation {
	case "like":
		return newState == 1 && oldState != 1
	case "unlike":
		return oldState == 1 && newState == 0
	case "dislike":
		return newState == 2 && oldState != 2
	case "undislike":
		return oldState == 2 && newState == 0
	default:
		return false
	}
}

func sameStringPointer(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func subjectID(subject communityfact.SubjectRef) int64 {
	id, _ := strconv.ParseInt(subject.SubjectID, 10, 64)
	return id
}
