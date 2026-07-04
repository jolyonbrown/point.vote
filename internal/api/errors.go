package api

import (
	"errors"
	"net/http"

	"github.com/jolyonbrown/point.vote/internal/room"
)

var errTooLarge = errors.New("request body exceeds 16KB")

// writeError maps domain errors to the wire shape
// {"error":{"code":"...","message":"..."}} with the right status code.
func writeError(w http.ResponseWriter, err error) {
	status, code := http.StatusInternalServerError, "internal"
	var verr room.ValidationError
	switch {
	case errors.As(err, &verr):
		status, code = http.StatusBadRequest, "validation"
	case errors.Is(err, room.ErrRoomNotFound):
		status, code = http.StatusNotFound, "not_found"
	case errors.Is(err, room.ErrBadToken):
		status, code = http.StatusUnauthorized, "bad_token"
	case errors.Is(err, room.ErrWrongState):
		status, code = http.StatusConflict, "wrong_state"
	case errors.Is(err, room.ErrRoomFull):
		status, code = http.StatusConflict, "room_full"
	case errors.Is(err, room.ErrObserverVote):
		status, code = http.StatusForbidden, "forbidden"
	case errors.Is(err, room.ErrServerFull):
		status, code = http.StatusServiceUnavailable, "server_full"
	case errors.Is(err, room.ErrTooFast):
		status, code = http.StatusTooManyRequests, "rate_limited"
	case errors.Is(err, errTooLarge):
		status, code = http.StatusRequestEntityTooLarge, "too_large"
	default:
		err = errors.New("internal error")
	}
	writeJSON(w, status, errBody(code, err.Error()))
}

func errBody(code, message string) map[string]any {
	return map[string]any{"error": map[string]string{"code": code, "message": message}}
}
