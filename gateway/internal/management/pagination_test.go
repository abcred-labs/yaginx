package management

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNormalizePageRequestUsesDefaultsAndRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name    string
		input   PageRequest
		want    PageRequest
		wantErr bool
	}{
		{name: "zero defaults", input: PageRequest{}, want: PageRequest{PageIndex: 1, PageSize: 50}},
		{name: "explicit maximum", input: PageRequest{PageIndex: 2, PageSize: 200}, want: PageRequest{PageIndex: 2, PageSize: 200}},
		{name: "negative page", input: PageRequest{PageIndex: -1, PageSize: 50}, wantErr: true},
		{name: "negative size", input: PageRequest{PageIndex: 1, PageSize: -1}, wantErr: true},
		{name: "too large", input: PageRequest{PageIndex: 1, PageSize: 201}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := NormalizePageRequest(test.input)
			if (err != nil) != test.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, test.wantErr)
			}
			if !test.wantErr && got != test.want {
				t.Fatalf("page=%#v want=%#v", got, test.want)
			}
		})
	}
}

func TestParsePageQueryRejectsMalformedValuesAndUsesFinalValue(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/resource_view/catalog?pageIndex=ignored&pageIndex=2&pageSize=%2050%20", nil)
	page, err := ParsePageQuery(request)
	if err != nil || page != (PageRequest{PageIndex: 2, PageSize: 50}) {
		t.Fatalf("page=%#v err=%v", page, err)
	}
	for _, query := range []string{"?pageIndex=bad", "?pageSize=201", "?pageIndex=-1", "?pageSize=-1"} {
		if _, err := ParsePageQuery(httptest.NewRequest(http.MethodGet, "/api/resource_view/catalog"+query, nil)); err == nil {
			t.Fatalf("query %s should be rejected", query)
		}
	}
}

func TestPageWindowClampsAndNormalizesEmptyResults(t *testing.T) {
	page, offset, err := PageWindow(PageRequest{PageIndex: 99, PageSize: 50}, 101)
	if err != nil || page.PageIndex != 3 || page.PageSize != 50 || page.Total != 101 || offset != 100 {
		t.Fatalf("page=%#v offset=%d err=%v", page, offset, err)
	}
	page, offset, err = PageWindow(PageRequest{PageIndex: 99, PageSize: 0}, 0)
	if err != nil || page.PageIndex != 1 || page.PageSize != 50 || page.Total != 0 || offset != 0 {
		t.Fatalf("empty page=%#v offset=%d err=%v", page, offset, err)
	}
}
