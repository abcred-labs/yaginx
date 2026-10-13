package management

import "net/http"

// DecodeBody is the common request boundary used by management handlers.
func DecodeBody[T any](r *http.Request, maximum int64) (T, error) {
	return DecodeTypedJSONBody[T](r.Body, maximum)
}

// DecodePagedBody decodes a request that embeds PageRequest and applies the
// same defaults and bounds as every paged management endpoint.
func DecodePagedBody[T interface{ PageRequestValue() *PageRequest }](r *http.Request, maximum int64) (T, error) {
	value, err := DecodeBody[T](r, maximum)
	if err != nil {
		return value, err
	}
	page, err := NormalizePageRequest(*value.PageRequestValue())
	if err != nil {
		return value, err
	}
	*value.PageRequestValue() = page
	return value, nil
}

// RequiredID is the shared query ID parser. RequiredIDQuery remains the
// compatibility name used by older handlers.
func RequiredID(r *http.Request, name string) (string, error) {
	return RequiredIDQuery(r, name)
}

func Respond[T any](w http.ResponseWriter, value T, err error) {
	RespondStore(w, value, err)
}

func RespondNullable[T any](w http.ResponseWriter, value T, err error) {
	RespondMissingAsNull(w, value, err)
}
