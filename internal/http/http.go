package http

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"

	"github.com/OpenSlides/openslides3-autoupdate-service/internal/autoupdate"
)

// Handler handels client requests to the autoupdate service.
type Handler struct {
	autoupdate *autoupdate.Autoupdate
	mux        *http.ServeMux
	auther     Auther
}

// New create a new Handler with the correct urls.
func New(autoupdate *autoupdate.Autoupdate, auther Auther) *Handler {
	h := &Handler{
		autoupdate: autoupdate,
		mux:        http.NewServeMux(),
		auther:     auther,
	}
	h.mux.Handle("/system/autoupdate", http.HandlerFunc(h.handleAutoupdate))
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

func (h *Handler) handleAutoupdate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/octet-stream")

	uid, err := h.auther.Auth(r)
	if err != nil {
		// TODO: Send a better message to the client when anonymous users are
		// not alowed.
		internalErr(w, fmt.Errorf("authenticate: %w", err))
		return
	}

	rawChangeID := r.URL.Query().Get("change_id")
	changeID := 0
	if rawChangeID != "" {
		var err error
		changeID, err = strconv.Atoi(rawChangeID)
		if err != nil {
			http.Error(w, fmt.Sprintf("change id has to be a number not %s", rawChangeID), http.StatusBadRequest)
			return
		}
	}

	w.WriteHeader(200)
	w.(http.Flusher).Flush()
	var data map[string]json.RawMessage
	var all bool
	var newChangeID int

	for {
		all, data, newChangeID, err = h.autoupdate.Receive(r.Context(), uid, changeID)
		if err != nil {
			internalErr(w, err)
			return
		}

		if data == nil {
			// Closing.
			return
		}

		if err := sendData(w, all, data, changeID, newChangeID); err != nil {
			internalErr(w, err)
			return
		}
		w.(http.Flusher).Flush()
		changeID = newChangeID
	}
}

func sendData(w io.Writer, all bool, data map[string]json.RawMessage, fromChangeID, toChangeID int) error {
	var deleted []string
	for k := range data {
		if data[k] == nil {
			deleted = append(deleted, k)
			delete(data, k)
		}
	}

	format := struct {
		Changed      map[string]json.RawMessage `json:"changed"`
		Deleted      []string                   `json:"deleted"`
		FromChangeID int                        `json:"from_change_id"`
		ToChangeID   int                        `json:"to_change_id"`
		AllData      bool                       `json:"all_data"`
	}{
		data,
		deleted,
		fromChangeID,
		toChangeID,
		all,
	}

	if err := json.NewEncoder(w).Encode(format); err != nil {
		return fmt.Errorf("encode and send output data: %w", err)
	}
	return nil
}

// internalErr sends a nonsense error message to the client and logs the real
// message to stdout.
func internalErr(w io.Writer, err error) {
	log.Printf("Internal Error: %v", err)
	fmt.Fprintln(w, `{"error": {"type": "InternalError", "msg": "Ups, something went wrong!"}}`)
}
