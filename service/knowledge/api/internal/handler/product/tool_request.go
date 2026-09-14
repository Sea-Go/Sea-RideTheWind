package product

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"

	"sea-try-go/service/knowledge/api/internal/model"
)

func decodeToolJSON(w http.ResponseWriter, r *http.Request, body any) error {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return fmt.Errorf("%w: application/json required", model.ErrInvalid)
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(body); err != nil {
		return fmt.Errorf("%w: malformed Tool request", model.ErrInvalid)
	}
	var tail any
	if decoder.Decode(&tail) != io.EOF {
		return fmt.Errorf("%w: trailing Tool request", model.ErrInvalid)
	}
	return nil
}
