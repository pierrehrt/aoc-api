package httpx

import "net/http"

// HealthBody is what GET /health returns. Deliberately tiny: it is read by uptime
// checks and by a human confirming which build is live, and by nothing else.
type HealthBody struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	Commit  string `json:"commit"`
	// Env is which environment answered. With one hosted environment that sounds
	// redundant, and is not: it is how you tell the live service from a local run or a
	// tunnelled port when a URL is ambiguous, and it is the field that starts earning its
	// keep the moment a second environment or a preview deploy exists.
	//
	// ⚠️ Added by AOC-004 verify round 4, which found ENV was documented in three places
	// (.env.example, docs/architecture.md § Configuration, Railway's own variables) and
	// read by NOTHING. .env.example even claimed "/health" read it. Either the claim went
	// or the variable did; reporting it makes all three true at once.
	Env string `json:"env"`
}

// Build is the identity of the running service. A struct rather than three string
// parameters in a row: ver, commit and env are all strings, so a mis-ordered call would
// compile cleanly and report a wrong build forever — on the one endpoint whose entire job
// is telling you what is running.
type Build struct {
	Version string
	Commit  string
	Env     string
}

// Health reports that the process is up and which build it is.
//
// It does NOT check the database. That is not an oversight: Railway restarts a
// container whose health check fails, and a database blip would then turn into a
// restart loop that takes the api down for reasons the api cannot fix. Readiness
// against Postgres is a separate endpoint when there is a reason for one (AOC-004).
func Health(b Build) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		Respond(w, r, http.StatusOK, HealthBody{
			Status: "ok", Version: b.Version, Commit: b.Commit, Env: b.Env,
		})
	}
}
