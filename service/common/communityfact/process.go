package communityfact

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
)

// Dispatcher is implemented by one RTW domain store. Each call claims and
// settles at most one transaction-frozen event.
type Dispatcher interface {
	DispatchFactOnce(context.Context, Sender, *slog.Logger) (bool, error)
}

// RunDispatcher drains bounded batches and keeps retry cadence outside the
// business RPC process. A failed attempt is retried with the same frozen event.
func RunDispatcher(ctx context.Context, dispatcher Dispatcher, sender Sender, logger *slog.Logger, once bool) error {
	started := time.Now()
	if dispatcher == nil || sender == nil || logger == nil {
		return errors.New("community fact dispatcher process is not configured")
	}
	dispatch := func() (bool, error) {
		attempt, span := otel.Tracer("sea.rtw.communityfact").Start(ctx, "community.fact_delivery.dispatch")
		defer span.End()
		attempt, cancel := context.WithTimeout(attempt, 10*time.Second)
		defer cancel()
		return dispatcher.DispatchFactOnce(attempt, sender, logger)
	}
	if once {
		sent, err := dispatch()
		if err != nil {
			return err
		}
		logger.InfoContext(ctx, "community fact delivery inspected", "event", "community.fact_delivery.once",
			"outcome", "succeeded", "duration_ms", float64(time.Since(started).Microseconds())/1000, "sent", sent)
		return nil
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		for i := 0; i < 16; i++ {
			sent, err := dispatch()
			if err != nil {
				logger.WarnContext(ctx, "community fact delivery retry scheduled",
					"event", "community.fact_delivery.retry", "outcome", "retryable",
					"error_code", deliveryErrorCode(err), "error_type", "delivery")
				break
			}
			if !sent {
				break
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func deliveryErrorCode(err error) string {
	if errors.Is(err, ErrFrozenEnvelopeBlocked) {
		return "INVALID_FROZEN_ENVELOPE"
	}
	return "DELIVERY_ATTEMPT_FAILED"
}
