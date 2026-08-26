package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
)

// decodeJSON decodes the request body into v. Returns an error on failure.
func decodeJSON(r *http.Request, v interface{}) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	return nil
}

// uuidParse parses a UUID string, returning uuid.Nil if invalid.
func uuidParse(s string) uuid.UUID {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil
	}
	return id
}

// notFoundError signals the resource does not exist.
var notFoundError = errors.New("not found")