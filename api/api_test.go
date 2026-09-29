package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Invalid bodies must be rejected before any docker service is touched, so nil
// services are enough here.
func TestCreateRejectsInvalidInput(t *testing.T) {
	h := NewHandler(nil, nil, nil)
	cases := map[string]string{
		`not json`:                         "invalid_input",
		`{"image":"x","bogus":1}`:          "invalid_input",
		`{}`:                               "validation_failed",
		`{"image":"x","env":["NOEQUALS"]}`: "validation_failed",
		`{"image":"x","memoryMB":-1}`:      "validation_failed",
	}
	for body, want := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/containers", strings.NewReader(body)))
		var got ErrorResponse
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatalf("%s: decoding body: %v", body, err)
		}
		if rec.Code != http.StatusBadRequest || got.Code != http.StatusBadRequest || got.Message != want {
			t.Errorf("%s: got %d %+v, want 400 %s", body, rec.Code, got, want)
		}
	}
}
