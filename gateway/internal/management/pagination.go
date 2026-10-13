package management

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
)

const (
	DefaultPageIndex = 1
	DefaultPageSize  = 50
	MaxPageSize      = 200
)

var ErrInvalidPagination = errors.New("invalid pagination")

// PageRequest is the shared wire and repository pagination input. Zero values
// are intentionally accepted as omitted values and normalize to the defaults;
// negative values and sizes above MaxPageSize are invalid.
type PageRequest struct {
	PageIndex int `json:"pageIndex"`
	PageSize  int `json:"pageSize"`
}

// PageRequestValue lets generic HTTP helpers normalize any request embedding
// the common pagination fields without knowing its concrete type.
func (p *PageRequest) PageRequestValue() *PageRequest { return p }

// PageCursor is the canonical pagination metadata returned by every paged
// management operation. Total is a JSON number, not a decimal string.
type PageCursor struct {
	PageIndex int   `json:"pageIndex"`
	PageSize  int   `json:"pageSize"`
	Total     int64 `json:"total"`
}

type Page[T any] struct {
	Records []T        `json:"records"`
	Paging  PageCursor `json:"paging"`
}

func NormalizePageRequest(value PageRequest) (PageRequest, error) {
	if value.PageIndex < 0 || value.PageSize < 0 || value.PageSize > MaxPageSize {
		return PageRequest{}, ErrInvalidPagination
	}
	if value.PageIndex == 0 {
		value.PageIndex = DefaultPageIndex
	}
	if value.PageSize == 0 {
		value.PageSize = DefaultPageSize
	}
	return value, nil
}

// PageWindow canonicalizes a request against the result count. Empty result
// sets always report page one. Requests beyond the last page fall back to the
// last page so list UIs remain usable after filtering or deletion.
func PageWindow(request PageRequest, total int64) (PageCursor, int64, error) {
	request, err := NormalizePageRequest(request)
	if err != nil {
		return PageCursor{}, 0, err
	}
	if total <= 0 {
		return PageCursor{PageIndex: DefaultPageIndex, PageSize: request.PageSize, Total: 0}, 0, nil
	}
	lastPage := (total-1)/int64(request.PageSize) + 1
	if int64(request.PageIndex) > lastPage {
		request.PageIndex = int(lastPage)
	}
	offset := int64(request.PageIndex-1) * int64(request.PageSize)
	return PageCursor{PageIndex: request.PageIndex, PageSize: request.PageSize, Total: total}, offset, nil
}

// ParsePageQuery follows the management API's repeated-query convention: the
// final value wins and surrounding whitespace is ignored. Missing, empty, and
// zero values use the shared defaults; malformed values are invalid requests.
func ParsePageQuery(r *http.Request) (PageRequest, error) {
	read := func(name string) (int, error) {
		values := r.URL.Query()[name]
		if len(values) == 0 {
			return 0, nil
		}
		raw := strings.TrimSpace(values[len(values)-1])
		if raw == "" {
			return 0, nil
		}
		value, err := strconv.Atoi(raw)
		if err != nil {
			return 0, ErrInvalidPagination
		}
		return value, nil
	}
	pageIndex, err := read("pageIndex")
	if err != nil {
		return PageRequest{}, err
	}
	pageSize, err := read("pageSize")
	if err != nil {
		return PageRequest{}, err
	}
	return NormalizePageRequest(PageRequest{PageIndex: pageIndex, PageSize: pageSize})
}
