package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
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

func TestCreateFromTemplate(t *testing.T) {
	h := NewHandler(nil, nil, nil)
	post := func(path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
		return rec
	}

	if rec := post("/templates/nope/containers", ""); rec.Code != http.StatusNotFound {
		t.Errorf("unknown template: got %d, want 404", rec.Code)
	}

	// rejected by validation, after the body env was merged into the template's
	want := slices.Clone(templates["minecraft-small"].Env)
	if rec := post("/templates/minecraft-small/containers", `{"env":["NOEQUALS"]}`); rec.Code != http.StatusBadRequest {
		t.Errorf("invalid override: got %d, want 400", rec.Code)
	}
	if got := templates["minecraft-small"].Env; !slices.Equal(got, want) {
		t.Errorf("template env mutated: got %v, want %v", got, want)
	}
}

func TestMergeEnv(t *testing.T) {
	got := mergeEnv([]string{"A=1", "AB=2"}, []string{"A=3", "C=4"})
	if want := []string{"A=3", "AB=2", "C=4"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
