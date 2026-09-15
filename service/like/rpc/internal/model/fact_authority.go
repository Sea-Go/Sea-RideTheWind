package model

import (
	"context"
	"strconv"
	"strings"
	"time"

	"sea-try-go/service/common/communityfact"
)

func deliveredLikeAuthority(row LikeDomainFactOutbox) (communityfact.AuthorityFact, likeFact, bool) {
	if row.DeliveryStatus != communityfact.DeliveryAccepted || row.DeliveredAt == nil ||
		row.TechnicalReceivedAt == nil || row.TechnicalReceiptID == "" || row.TechnicalOffset < 1 ||
		!communityfact.ValidHash(row.TechnicalInputHash) || row.DeliveryEnvelope == nil {
		return communityfact.AuthorityFact{}, likeFact{}, false
	}
	var event communityfact.Event
	if err := communityfact.StrictDecode(strings.NewReader(*row.DeliveryEnvelope), &event); err != nil ||
		!validLikeDelivery(row, event) {
		return communityfact.AuthorityFact{}, likeFact{}, false
	}
	var payload likeFact
	if err := communityfact.StrictDecode(strings.NewReader(string(event.Payload)), &payload); err != nil {
		return communityfact.AuthorityFact{}, likeFact{}, false
	}
	subject, ok := communityfact.ParseRTWSubject(payload.SubjectRef)
	if !ok || !validLikeRevision(payload.TargetRevision, payload.RevisionStatus) ||
		payload.SourceRef != payload.EventID || payload.EventTime.IsZero() || payload.AvailableAt.IsZero() ||
		payload.EventType != "community.target.interaction" ||
		payload.AggregateID != payload.TargetType+"/"+payload.TargetID ||
		payload.EventID != "rtw.like."+payload.OperationID || event.OperationID != payload.OperationID {
		return communityfact.AuthorityFact{}, likeFact{}, false
	}
	hash, err := communityfact.CanonicalHash([]byte(*row.DeliveryEnvelope))
	if err != nil || hash != row.TechnicalInputHash {
		return communityfact.AuthorityFact{}, likeFact{}, false
	}
	receipt := communityfact.TechnicalReceipt{EventID: row.EventID, Producer: event.Producer,
		TechnicalStatus: "accepted", ReceiptID: row.TechnicalReceiptID, InputHash: row.TechnicalInputHash,
		Offset: row.TechnicalOffset, ReceivedAt: row.TechnicalReceivedAt.UTC().Format(time.RFC3339Nano)}
	return communityfact.AuthorityFact{Event: event, SubjectRef: subject,
		TechnicalReceipt: receipt, SourceEventHash: hash}, payload, true
}

func validLikeRevision(revision *string, status string) bool {
	if revision == nil {
		return status == "unknown"
	}
	return *revision != "" && status == "resolved"
}

func (m *LikeFactModel) AuthoritativeFact(ctx context.Context, producer, eventID string) (communityfact.AuthorityFact, error) {
	if m == nil || m.db == nil || producer != likeFactProducer || eventID == "" || len(eventID) > 128 {
		return communityfact.AuthorityFact{}, communityfact.ErrAuthorityUnavailable
	}
	var requested LikeDomainFactOutbox
	query := m.db.WithContext(ctx).Where("event_id = ?", eventID).Limit(1).Find(&requested)
	if query.Error != nil {
		return communityfact.AuthorityFact{}, query.Error
	}
	if query.RowsAffected != 1 {
		return communityfact.AuthorityFact{}, communityfact.ErrAuthorityUnavailable
	}
	authority, payload, ok := deliveredLikeAuthority(requested)
	if !ok || requested.AggregateID == nil || requested.FactVersion == nil {
		return communityfact.AuthorityFact{}, communityfact.ErrAuthorityUnavailable
	}
	var rows []LikeDomainFactOutbox
	if err := m.db.WithContext(ctx).Where("aggregate_id = ?", *requested.AggregateID).
		Order("fact_version,event_id").Find(&rows).Error; err != nil {
		return communityfact.AuthorityFact{}, err
	}
	state := int32(0)
	previous := ""
	found := false
	for index, candidate := range rows {
		if candidate.DeliveryEnvelope == nil || candidate.FactVersion == nil || *candidate.FactVersion != int64(index+1) {
			return communityfact.AuthorityFact{}, communityfact.ErrAuthorityUnavailable
		}
		var event communityfact.Event
		if err := communityfact.StrictDecode(strings.NewReader(*candidate.DeliveryEnvelope), &event); err != nil ||
			!validLikeDelivery(candidate, event) {
			return communityfact.AuthorityFact{}, communityfact.ErrAuthorityUnavailable
		}
		var current likeFact
		if err := communityfact.StrictDecode(strings.NewReader(string(event.Payload)), &current); err != nil ||
			current.OldState != state || current.SubjectRef != payload.SubjectRef ||
			current.TargetType != payload.TargetType || current.TargetID != payload.TargetID ||
			!sameLikeRevision(current.TargetRevision, payload.TargetRevision) ||
			!validLikeTransition(current.Operation, current.OldState, current.NewState) {
			return communityfact.AuthorityFact{}, communityfact.ErrAuthorityUnavailable
		}
		if candidate.EventID == eventID {
			found = true
			if current.OldState != 0 {
				authority.PredecessorEventID = previous
			}
		}
		state = current.NewState
		previous = candidate.EventID
	}
	if !found || (payload.OldState != 0 && authority.PredecessorEventID == "") {
		return communityfact.AuthorityFact{}, communityfact.ErrAuthorityUnavailable
	}
	uid := subjectIDFromRef(authority.SubjectRef)
	var record LikeRecord
	lookup := m.db.WithContext(ctx).Where("user_id = ? AND target_type = ? AND target_id = ?",
		uid, payload.TargetType, payload.TargetID).Limit(1).Find(&record)
	if lookup.Error != nil {
		return communityfact.AuthorityFact{}, lookup.Error
	}
	operationID, err := strconv.ParseInt(payload.OperationID, 10, 64)
	if lookup.RowsAffected != 1 || err != nil || operationID <= 0 || record.State != state ||
		record.LastOperationID < operationID || record.UserID != uid {
		return communityfact.AuthorityFact{}, communityfact.ErrAuthorityUnavailable
	}
	return authority, nil
}

func validLikeTransition(operation string, oldState, newState int32) bool {
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

func sameLikeRevision(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func subjectIDFromRef(subject communityfact.SubjectRef) int64 {
	id, _ := strconv.ParseInt(subject.SubjectID, 10, 64)
	return id
}
