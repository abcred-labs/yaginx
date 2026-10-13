package history

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"yaginx-gateway/internal/management"
)

type captureStore struct {
	query management.RequestHistorySearchQuery
}

func (s *captureStore) SearchRequestHistory(_ context.Context, query management.RequestHistorySearchQuery) (management.RequestHistoryPage, error) {
	s.query = query
	return management.RequestHistoryPage{Records: []management.RequestHistoryListItem{}, Paging: management.PageCursor{PageIndex: 1, PageSize: 50}}, nil
}

func (s *captureStore) GetRequestHistory(context.Context, string) (management.RequestHistoryDetail, error) {
	return management.RequestHistoryDetail{}, management.ErrNotFound
}

func serveSearch(t *testing.T, store *captureStore, body string) *httptest.ResponseRecorder {
	t.Helper()
	router := chi.NewRouter()
	Handler{Store: store, BodyMax: management.DefaultBodyMax}.Routes(router)
	request := httptest.NewRequest(http.MethodPost, "/request_history/search", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestSearchAcceptsNumericAndStringTimeFilters(t *testing.T) {
	tests := map[string]string{
		"numeric": `{"from":1791219685,"to":1791306085}`,
		"string":  `{"from":"1791219685","to":"1791306085"}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			store := &captureStore{}
			response := serveSearch(t, store, body)
			if response.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if store.query.From == nil || store.query.To == nil || int64(*store.query.From) != 1791219685 || int64(*store.query.To) != 1791306085 {
				t.Fatalf("time filters did not reach the store: %#v", store.query)
			}
		})
	}
}

func TestSearchRejectsInvalidTimeFilter(t *testing.T) {
	response := serveSearch(t, &captureStore{}, `{"from":"not-a-timestamp"}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
