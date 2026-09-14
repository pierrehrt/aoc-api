package httpx

import "net/http"

// HealthBody is what GET /health returns. Deliberately tiny: it is read by uptime
// checks and by a human confirming which build is live, and by nothing else.
type HealthBody struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

// Health reports that the process is up and which build it is.
//
// It does NOT check the database. That is not an oversight: Railway restarts a
// container whose health check fails, and a database blip would then turn into a
// restart loop that takes the api down for reasons the api cannot fix. Readiness
// against Postgres is a separate endpoint when there is a reason for one (AOC-004).
func Health(ver, commit string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		Respond(w, r, http.StatusOK, HealthBody{Status: "ok", Version: ver, Commit: commit})
	}
}
