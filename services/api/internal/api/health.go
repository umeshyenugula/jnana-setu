package api

import (
	"encoding/json"
	"net/http"
)

const APIVersion = "v1"

type healthData struct {
	Status  string `json:"status"`
	Service string `json:"service"`
	Version string `json:"version"`
}

// Health returns the minimal liveness response for the API process.
func Health(w http.ResponseWriter, r *http.Request, correlationID string, version string) {
	data := healthData{
		Status:  "ok",
		Service: "api",
		Version: version,
	}
	WriteSuccess(w, r, http.StatusOK, data, correlationID, APIVersion)
}

// DecodeJSON is a helper reserved for future request validation.
func DecodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}
