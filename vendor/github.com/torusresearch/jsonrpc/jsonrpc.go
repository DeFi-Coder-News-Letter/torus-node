package jsonrpc

import (
	"bytes"
	"net/http"
	"strings"

	"github.com/torusresearch/bijson"
)

const (
	// Version is JSON-RPC 2.0.
	Version = "2.0"

	batchRequestKey  = '['
	contentTypeKey   = "Content-Type"
	contentTypeValue = "application/json"
)

type (
	// A Request represents a JSON-RPC request received by the server.
	Request struct {
		Version string             `json:"jsonrpc"`
		Method  string             `json:"method"`
		Params  *bijson.RawMessage `json:"params"`
		ID      *bijson.RawMessage `json:"id"`
	}

	// A Response represents a JSON-RPC response returned by the server.
	Response struct {
		Version string             `json:"jsonrpc"`
		Result  interface{}        `json:"result,omitempty"`
		Error   *Error             `json:"error,omitempty"`
		ID      *bijson.RawMessage `json:"id,omitempty"`
	}
)

// ParseRequest parses a HTTP request to JSON-RPC request.
func ParseRequest(r *http.Request) ([]*Request, bool, *Error) {

	var rerr *Error

	if !strings.HasPrefix(r.Header.Get(contentTypeKey), contentTypeValue) {
		return nil, false, ErrInvalidRequest()
	}

	buf := bytes.NewBuffer(make([]byte, 0, r.ContentLength))
	if _, err := buf.ReadFrom(r.Body); err != nil {
		return nil, false, ErrInvalidRequest()
	}
	defer func(r *http.Request) {
		err := r.Body.Close()
		if err != nil {
			rerr = ErrInternal()
		}
	}(r)

	if buf.Len() == 0 {
		return nil, false, ErrInvalidRequest()
	}

	f, _, err := buf.ReadRune()
	if err != nil {
		return nil, false, ErrInvalidRequest()
	}
	if err := buf.UnreadRune(); err != nil {
		return nil, false, ErrInvalidRequest()
	}

	var rs []*Request
	if f != batchRequestKey {
		var req *Request
		if err := bijson.NewDecoder(buf).Decode(&req); err != nil {
			return nil, false, ErrParse()
		}
		return append(rs, req), false, nil
	}

	if err := bijson.NewDecoder(buf).Decode(&rs); err != nil {
		return nil, false, ErrParse()
	}

	return rs, true, rerr
}

// NewResponse generates a JSON-RPC response.
func NewResponse(r *Request) *Response {
	return &Response{
		Version: r.Version,
		ID:      r.ID,
	}
}

// SendResponse writes JSON-RPC response.
func SendResponse(w http.ResponseWriter, resp []*Response, batch bool) error {
	w.Header().Set(contentTypeKey, contentTypeValue)
	if batch || len(resp) > 1 {
		return bijson.NewEncoder(w).Encode(resp)
	} else if len(resp) == 1 {
		return bijson.NewEncoder(w).Encode(resp[0])
	}
	return nil
}